package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type Searcher interface {
	SearchMessages(context.Context, string, int) ([]store.Message, error)
}
type Server struct {
	search    Searcher
	state     *health.State
	freshness time.Duration
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type callParams struct {
	Name      string `json:"name"`
	Arguments struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	} `json:"arguments"`
}

func New(search Searcher, state *health.State, freshness time.Duration) *Server {
	return &Server{search: search, state: state, freshness: freshness}
}

func (s *Server) Handle(ctx context.Context, line []byte) []byte {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return encode(nil, nil, rpcError(-32700, "parse error"))
	}
	switch req.Method {
	case "initialize":
		return encode(req.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "whatsapp-mcp", "version": "0.1.0"}}, nil)
	case "notifications/initialized":
		return nil
	case "tools/list":
		tools := []any{
			map[string]any{"name": "whatsapp_status", "description": "Report WhatsApp ingestion connection and freshness status", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			map[string]any{"name": "search_messages", "description": "Search locally persisted WhatsApp messages; refuses when freshness cannot be trusted", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "required": []string{"query"}}},
		}
		return encode(req.ID, map[string]any{"tools": tools}, nil)
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

func (s *Server) call(ctx context.Context, params callParams) map[string]any {
	snapshot := s.state.Snapshot()
	stale := snapshot.Stale(s.freshness)
	status := map[string]any{"evolution_connected": snapshot.EvolutionConnected, "last_event_at": snapshot.LastEventAt, "rabbit_connected": snapshot.RabbitConnected, "database_connected": snapshot.DatabaseConnected, "stale": stale}
	if stale {
		status["warning"] = health.FreshnessWarning
	}
	if params.Name == "whatsapp_status" {
		return textResult(status, false)
	}
	if params.Name != "search_messages" {
		return textResult(map[string]any{"error": "unknown tool"}, true)
	}
	if stale || !snapshot.EvolutionConnected {
		return textResult(map[string]any{"error": "search refused because message freshness cannot be trusted", "warning": health.FreshnessWarning, "status": status}, true)
	}
	messages, err := s.search.SearchMessages(ctx, params.Arguments.Query, params.Arguments.Limit)
	if err != nil {
		return textResult(map[string]any{"error": err.Error()}, true)
	}
	return textResult(map[string]any{"messages": messages, "freshness_trustworthy": true}, false)
}

func textResult(value any, isError bool) map[string]any {
	body, _ := json.Marshal(value)
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(body)}}, "isError": isError}
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
