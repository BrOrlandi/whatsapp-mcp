// Package mcp implements the MCP server: the tool surface, the session that
// binds every call to one authorised WhatsApp instance, and the JSON-RPC
// plumbing shared by the stdio and HTTP transports.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Index is the message history, which lives in PostgreSQL because Evolution Go
// exposes no route to list conversations or read past messages.
type Index interface {
	SelectedInstance(context.Context) (string, error)
	ManagedInstances(context.Context) (map[string]string, error)
	InstanceToken(context.Context, string) (string, error)
	Coverage(context.Context, string) (store.Coverage, error)
	ListChats(context.Context, string, string, int) ([]store.Chat, error)
	Messages(context.Context, string, store.MessageQuery) ([]store.Message, error)
	OldestMessage(context.Context, string, string) (store.Message, error)
	IndexGaps(context.Context, string, time.Duration, int) ([]store.Gap, error)
	GapAnchors(context.Context, string, string, time.Time, int) ([]store.Message, error)
	ChatsWithoutAnchor(context.Context, string, time.Time) (int64, error)
	RawMessage(context.Context, string, string) ([]byte, error)
}

// Live is the part of WhatsApp that Evolution answers for: the address book,
// the groups, and everything that changes the world.
type Live interface {
	Contacts(context.Context, string) ([]evolution.Contact, error)
	Groups(context.Context, string) ([]evolution.Group, error)
	Group(context.Context, string, string) (evolution.Group, error)
	SendText(context.Context, string, string, string) (evolution.SentMessage, error)
	SendMedia(context.Context, string, string, string, string, string, string) (evolution.SentMessage, error)
	WarmSession(context.Context, string, string) error
	Delivered(context.Context, string, string) (evolution.Delivery, error)
	DownloadMedia(context.Context, string, json.RawMessage) (evolution.Media, error)
	RequestHistory(context.Context, string, evolution.Anchor, int) error
}

// Session is the authorised instance a call runs against. It is resolved from
// the credential, never from a tool argument, so a client cannot reach an
// instance its key was not issued for.
type Session struct {
	InstanceID   string
	InstanceName string
	Token        string
}

type Server struct {
	index     Index
	live      Live
	state     *health.State
	freshness time.Duration
}

func New(index Index, live Live, state *health.State, freshness time.Duration) *Server {
	return &Server{index: index, live: live, state: state, freshness: freshness}
}

// sessionKey carries the authorised instance through the context, which keeps
// the JSON-RPC layer free of transport-specific plumbing.
type sessionKey struct{}

// WithSession binds a request context to an authorised instance.
func WithSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// Session resolves the instance for this call. A transport that authenticated a
// credential has already put one in the context; stdio, which is a local
// development transport with no credential, falls back to the instance the
// panel selected.
func (s *Server) Session(ctx context.Context) (Session, error) {
	if session, ok := ctx.Value(sessionKey{}).(Session); ok && session.InstanceID != "" {
		return session, nil
	}
	selected, err := s.index.SelectedInstance(ctx)
	if err != nil {
		return Session{}, err
	}
	if selected == "" {
		return Session{}, errors.New("no WhatsApp instance is selected in the control panel")
	}
	return s.resolve(ctx, selected)
}

// resolve fills in the instance name and the Evolution token. The token is an
// internal secret and never leaves the process.
func (s *Server) resolve(ctx context.Context, instanceID string) (Session, error) {
	session := Session{InstanceID: instanceID}
	token, err := s.index.InstanceToken(ctx, instanceID)
	if err != nil {
		return session, fmt.Errorf("this gateway holds no credentials for instance %s", instanceID)
	}
	session.Token = token
	if managed, err := s.index.ManagedInstances(ctx); err == nil {
		session.InstanceName = managed[instanceID]
	}
	return session, nil
}

// Resolve builds the session for an authenticated instance. Transports call it
// after verifying a credential.
func (s *Server) Resolve(ctx context.Context, instanceID string) (Session, error) {
	return s.resolve(ctx, instanceID)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) Handle(ctx context.Context, line []byte) []byte {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return encode(nil, nil, rpcError(-32700, "parse error"))
	}
	switch req.Method {
	case "initialize":
		return encode(req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "whatsapp-mcp", "version": "0.2.0"},
		}, nil)
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return encode(req.ID, map[string]any{}, nil)
	case "tools/list":
		return encode(req.ID, map[string]any{"tools": toolDefinitions()}, nil)
	case "tools/call":
		var params callParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return encode(req.ID, nil, rpcError(-32602, "invalid params"))
		}
		return encode(req.ID, s.call(ctx, params), nil)
	default:
		return encode(req.ID, nil, rpcError(-32601, "method not found"))
	}
}

func textResult(value any, isError bool) map[string]any {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		body = []byte(`{"error":"failed to encode the tool result"}`)
		isError = true
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(body)}}, "isError": isError}
}

func toolError(format string, args ...any) map[string]any {
	return textResult(map[string]any{"error": fmt.Sprintf(format, args...)}, true)
}

func rpcError(code int, message string) map[string]any {
	return map[string]any{"code": code, "message": message}
}

func encode(id json.RawMessage, result any, err any) []byte {
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		response["error"] = err
	} else {
		response["result"] = result
	}
	body, _ := json.Marshal(response)
	return body
}

// Serve runs the newline-delimited stdio transport, which exists for local
// development. The supported path is the authenticated HTTP endpoint.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(out)
	defer writer.Flush()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if response := s.Handle(ctx, scanner.Bytes()); len(response) > 0 {
			if _, err := fmt.Fprintln(writer, string(response)); err != nil {
				return err
			}
			if err := writer.Flush(); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
