package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeSelection struct {
	selected    string
	selectedErr error
	coverage    store.Coverage
	coverageErr error
}

func (f fakeSelection) SelectedInstance(context.Context) (string, error) {
	return f.selected, f.selectedErr
}
func (f fakeSelection) Coverage(context.Context, string) (store.Coverage, error) {
	return f.coverage, f.coverageErr
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The bug this fixes, in the deployment that reported it: a restart wipes the
// in-memory last-event time, the first poll of Evolution's stale instance
// record then has nothing to weigh against it, and the panel announced a
// disconnected WhatsApp on a session that had delivered a message twenty-eight
// seconds before the restart. The index remembers what the process forgot.
func TestSeedLastEventRecoversTheEvidenceARestartWiped(t *testing.T) {
	newest := time.Now().Add(-30 * time.Second).UTC().Truncate(time.Second)
	state := health.NewState()
	seedLastEvent(context.Background(), fakeSelection{selected: "inst-1", coverage: store.Coverage{NewestAt: newest}}, state, quietLogger())

	snapshot := state.Snapshot()
	if !snapshot.LastEventAt.Equal(newest) {
		t.Fatalf("last event = %s, want the newest indexed message %s", snapshot.LastEventAt, newest)
	}

	// Which is the whole point: the very first poll now has something to
	// contradict, so a stale record cannot take the session down.
	if !state.ObserveInstance(false, 5*time.Minute) {
		t.Fatal("the first poll after a restart was still believed unopposed")
	}
	if got := state.Snapshot().WhatsApp.State; got != "connected" {
		t.Fatalf("session reported %q after recovering its evidence", got)
	}
}

// An empty index is not evidence of anything, and must not be read as an event
// at the zero time — which would look like the oldest possible traffic.
func TestSeedLastEventIgnoresAnEmptyIndex(t *testing.T) {
	state := health.NewState()
	seedLastEvent(context.Background(), fakeSelection{selected: "inst-1"}, state, quietLogger())
	if got := state.Snapshot().LastEventAt; !got.IsZero() {
		t.Fatalf("last event = %s, want zero for an index with nothing in it", got)
	}
}

// Nothing here is worth refusing to start over: without the seed the first
// minutes are judged by the poll alone, which is where this started.
func TestSeedLastEventSurvivesAStoreThatCannotAnswer(t *testing.T) {
	for _, selection := range []fakeSelection{
		{selectedErr: errors.New("no database")},
		{selected: ""},
		{selected: "inst-1", coverageErr: errors.New("query failed")},
	} {
		state := health.NewState()
		seedLastEvent(context.Background(), selection, state, quietLogger())
		if got := state.Snapshot().LastEventAt; !got.IsZero() {
			t.Fatalf("last event = %s, want zero when the index could not be read", got)
		}
	}
}
