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

// Index reports what the message index holds, so a tool can say "not indexed"
// instead of letting the caller conclude "does not exist".
type Index interface {
	SelectedInstance(context.Context) (string, error)
	ManagedInstances(context.Context) (map[string]string, error)
	Coverage(context.Context, string) (store.Coverage, error)
}

type Server struct {
	search    Searcher
	index     Index
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

func New(search Searcher, index Index, state *health.State, freshness time.Duration) *Server {
	return &Server{search: search, index: index, state: state, freshness: freshness}
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
			map[string]any{"name": "whatsapp_status", "description": "Report the WhatsApp session state, the ingestion queues, the index coverage and any problem that needs attention", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			map[string]any{"name": "search_messages", "description": "Search indexed WhatsApp messages. Always answers, and reports how far back the index goes so an absent result is not mistaken for an absent conversation", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "required": []string{"query"}}},
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

// status is the single picture both tools report from: what WhatsApp says, what
// the gateway is doing, and how much history the index actually covers.
func (s *Server) status(ctx context.Context) map[string]any {
	snapshot := s.state.Snapshot()
	stale := snapshot.Stale(s.freshness)
	status := map[string]any{
		"whatsapp": snapshot.WhatsApp,
		"gateway": map[string]any{
			"evolution_reachable": snapshot.EvolutionConnected,
			"queue_connected":     snapshot.RabbitConnected,
			"database_connected":  snapshot.DatabaseConnected,
		},
		"queues":          snapshot.Queues,
		"last_event_at":   snapshot.LastEventAt,
		"last_message_at": snapshot.LastMessageAt,
		"stale":           stale,
		"ready":           snapshot.Ready(s.freshness),
	}
	if !snapshot.LastHistoryAt.IsZero() {
		status["last_history_sync_at"] = snapshot.LastHistoryAt
	}
	if problems := snapshot.Problems(); len(problems) > 0 {
		status["problems"] = problems
	}
	if stale {
		status["warning"] = health.FreshnessWarning
	}
	instance, coverage := s.coverage(ctx)
	if instance != nil {
		status["instance"] = instance
	}
	if coverage != nil {
		status["index"] = coverage
	}
	return status
}

// coverage describes the selected instance and what the index holds for it.
// Both are best effort: a status report must still answer when the database is
// the thing that is broken.
func (s *Server) coverage(ctx context.Context) (map[string]any, map[string]any) {
	if s.index == nil {
		return nil, nil
	}
	selected, err := s.index.SelectedInstance(ctx)
	if err != nil || selected == "" {
		return nil, nil
	}
	instance := map[string]any{"id": selected}
	if managed, err := s.index.ManagedInstances(ctx); err == nil {
		if name := managed[selected]; name != "" {
			instance["name"] = name
		}
	}
	stats, err := s.index.Coverage(ctx, selected)
	if err != nil {
		return instance, nil
	}
	index := map[string]any{"messages": stats.Messages}
	if !stats.OldestAt.IsZero() {
		index["history_since"] = stats.OldestAt
	}
	if !stats.NewestAt.IsZero() {
		index["newest_message_at"] = stats.NewestAt
	}
	return instance, index
}

func (s *Server) call(ctx context.Context, params callParams) map[string]any {
	status := s.status(ctx)
	if params.Name == "whatsapp_status" {
		return textResult(status, false)
	}
	if params.Name != "search_messages" {
		return textResult(map[string]any{"error": "unknown tool"}, true)
	}
	messages, err := s.search.SearchMessages(ctx, params.Arguments.Query, params.Arguments.Limit)
	if err != nil {
		return textResult(map[string]any{"error": err.Error(), "status": status}, true)
	}
	// The search answers even when the pipeline is degraded. Refusing would hide
	// the messages that are indexed; reporting the coverage lets the caller judge
	// whether an empty result means "nothing was said" or "nothing was indexed".
	result := map[string]any{"messages": messages, "count": len(messages)}
	if index, ok := status["index"]; ok {
		result["index"] = index
	}
	if problems, ok := status["problems"]; ok {
		result["problems"] = problems
		result["warning"] = health.FreshnessWarning
	} else if status["stale"] == true {
		result["warning"] = health.FreshnessWarning
	}
	return textResult(result, false)
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
