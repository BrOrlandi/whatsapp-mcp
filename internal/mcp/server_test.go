package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeSearch struct {
	called   bool
	messages []store.Message
	err      error
}

func (f *fakeSearch) SearchMessages(context.Context, string, int) ([]store.Message, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	if f.messages == nil {
		return []store.Message{{MessageID: "1", Text: "hello"}}, nil
	}
	return f.messages, nil
}

type fakeIndex struct {
	selected string
	names    map[string]string
	coverage store.Coverage
	err      error
}

func (f fakeIndex) SelectedInstance(context.Context) (string, error) { return f.selected, nil }
func (f fakeIndex) ManagedInstances(context.Context) (map[string]string, error) {
	return f.names, nil
}
func (f fakeIndex) Coverage(context.Context, string) (store.Coverage, error) {
	return f.coverage, f.err
}

// call runs one tool and returns the decoded payload plus whether the tool
// reported an error.
func call(t *testing.T, s *Server, name string) (map[string]any, bool) {
	t.Helper()
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{"query":"hello"}}}`
	var response map[string]any
	if err := json.Unmarshal(s.Handle(context.Background(), []byte(line)), &response); err != nil {
		t.Fatal(err)
	}
	result := response["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("tool payload is not JSON: %s", text)
	}
	return payload, result["isError"] == true
}

// A degraded pipeline must not hide the messages that are indexed. Refusing
// outright was the old behaviour and it made a quiet account and a broken
// ingestion indistinguishable to the caller.
func TestSearchAnswersWhileDegradedAndReportsWhy(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(false, false, true)
	state.SetWhatsApp("logged_out", "401", "", "")
	search := &fakeSearch{}
	server := New(search, fakeIndex{selected: "inst-1", coverage: store.Coverage{Messages: 3}}, state, time.Minute)

	payload, isError := call(t, server, "search_messages")
	if isError || !search.called {
		t.Fatalf("search was refused: %#v", payload)
	}
	if payload["warning"] == nil {
		t.Fatalf("degraded search carried no warning: %#v", payload)
	}
	problems, _ := payload["problems"].([]any)
	joined := ""
	for _, problem := range problems {
		joined += problem.(string) + "|"
	}
	if !strings.Contains(joined, "encerrada") || !strings.Contains(joined, "fila") {
		t.Fatalf("problems did not explain the failure: %q", joined)
	}
}

// An empty result is ambiguous unless the caller knows how far back the index
// reaches, so coverage travels with the answer.
func TestSearchReportsIndexCoverage(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(true, true, true)
	state.MarkEvent(time.Now())
	oldest := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	server := New(&fakeSearch{messages: []store.Message{}}, fakeIndex{selected: "inst-1", coverage: store.Coverage{Messages: 12, OldestAt: oldest}}, state, time.Minute)

	payload, isError := call(t, server, "search_messages")
	if isError {
		t.Fatalf("healthy search reported an error: %#v", payload)
	}
	index, ok := payload["index"].(map[string]any)
	if !ok {
		t.Fatalf("no coverage in answer: %#v", payload)
	}
	if index["messages"].(float64) != 12 || index["history_since"] == nil {
		t.Fatalf("coverage = %#v", index)
	}
	if payload["warning"] != nil {
		t.Fatalf("healthy search warned anyway: %#v", payload)
	}
}

// The status tool is what the operator and the client consult when something is
// wrong, so it must answer even while everything else is failing.
func TestStatusAlwaysAnswersAndNamesTheInstance(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(false, false, false)
	state.SetWhatsApp("banned", "spam", "5511@s.whatsapp.net", "Eu")
	state.SetQueueConsuming("message", false, "connection refused")
	state.SetQueueConsuming("historysync", true, "")
	state.MarkQueueEvent("historysync", time.Now(), false)
	index := fakeIndex{selected: "inst-1", names: map[string]string{"inst-1": "pessoal"}, err: errors.New("database down")}
	server := New(&fakeSearch{}, index, state, time.Minute)

	payload, isError := call(t, server, "whatsapp_status")
	if isError {
		t.Fatalf("status reported an error: %#v", payload)
	}
	instance := payload["instance"].(map[string]any)
	if instance["id"] != "inst-1" || instance["name"] != "pessoal" {
		t.Fatalf("instance = %#v", instance)
	}
	whatsapp := payload["whatsapp"].(map[string]any)
	if whatsapp["state"] != "banned" || whatsapp["reason"] != "spam" || whatsapp["push_name"] != "Eu" {
		t.Fatalf("whatsapp = %#v", whatsapp)
	}
	queues := payload["queues"].([]any)
	if len(queues) != 2 {
		t.Fatalf("queues = %#v", queues)
	}
	first := queues[0].(map[string]any)
	if first["name"] != "historysync" || first["consuming"] != true {
		t.Fatalf("queue = %#v", first)
	}
	problems := payload["problems"].([]any)
	if len(problems) == 0 {
		t.Fatalf("status hid the problems: %#v", payload)
	}
	if payload["ready"] != false {
		t.Fatalf("ready = %#v", payload["ready"])
	}
}

// A queue that Evolution declares but nobody consumes grows without bound, so a
// stopped consumer has to surface as a problem rather than as silence.
func TestStatusFlagsAQueueThatIsNotBeingConsumed(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(true, true, true)
	state.MarkEvent(time.Now())
	state.SetQueueConsuming("sendmessage", false, "channel closed")
	server := New(&fakeSearch{}, fakeIndex{}, state, time.Minute)

	payload, _ := call(t, server, "whatsapp_status")
	problems := payload["problems"].([]any)
	found := false
	for _, problem := range problems {
		if strings.Contains(problem.(string), "sendmessage") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unconsumed queue not reported: %#v", problems)
	}
}

func TestToolsListContainsProductionSliceTools(t *testing.T) {
	s := New(&fakeSearch{}, fakeIndex{}, health.NewState(), time.Minute)
	response := string(s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if !strings.Contains(response, "whatsapp_status") || !strings.Contains(response, "search_messages") {
		t.Fatal(response)
	}
}
