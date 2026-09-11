package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeSearch struct{ called bool }

func (f *fakeSearch) SearchMessages(context.Context, string, int) ([]store.Message, error) {
	f.called = true
	return []store.Message{{MessageID: "1", Text: "hello"}}, nil
}

func TestSearchRefusesWhenFreshnessIsUntrustworthy(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(false, true, true)
	f := &fakeSearch{}
	s := New(f, state, time.Minute)
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_messages","arguments":{"query":"hello"}}}`
	var response map[string]any
	if err := json.Unmarshal(s.Handle(context.Background(), []byte(line)), &response); err != nil {
		t.Fatal(err)
	}
	result := response["result"].(map[string]any)
	if result["isError"] != true || f.called {
		t.Fatalf("expected refusal without search: %#v", response)
	}
	content := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(content, "freshness") {
		t.Fatalf("missing freshness warning: %s", content)
	}
}

func TestToolsListContainsProductionSliceTools(t *testing.T) {
	s := New(&fakeSearch{}, health.NewState(), time.Minute)
	response := string(s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if !strings.Contains(response, "whatsapp_status") || !strings.Contains(response, "search_messages") {
		t.Fatal(response)
	}
}
