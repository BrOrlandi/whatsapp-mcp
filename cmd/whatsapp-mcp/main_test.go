package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
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

type fakeLister struct {
	instances []evolution.Instance
	checkErr  error
	checked   int
	token     string
	numbers   []string
}

func (f *fakeLister) FetchInstances(context.Context) ([]evolution.Instance, error) {
	return f.instances, nil
}
func (f *fakeLister) CheckNumbers(_ context.Context, token string, numbers []string) ([]evolution.Presence, error) {
	f.checked++
	f.token, f.numbers = token, numbers
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return []evolution.Presence{{Number: numbers[0], JID: "55@s.whatsapp.net", OnWhatsApp: true}}, nil
}

// Answering "does this number have WhatsApp" requires the client to be
// connected to WhatsApp, so a reply is proof of a live session whatever
// Evolution's own record claims. The answer itself is not used.
func TestProbeSessionTreatsAnyAnswerAsProofOfLife(t *testing.T) {
	lister := &fakeLister{}
	instance := evolution.Instance{ID: "inst-1", Token: "tok", Number: "5511923456789"}

	if !probeSession(context.Background(), lister, instance) {
		t.Fatal("a WhatsApp answer was not accepted as proof of a live session")
	}
	if lister.token != "tok" || len(lister.numbers) != 1 || lister.numbers[0] != "5511923456789" {
		t.Fatalf("probe asked with token %q and numbers %v", lister.token, lister.numbers)
	}
}

// An error proves nothing either way — it could be the session, the network or
// Evolution — so it must not be read as a live session.
func TestProbeSessionDoesNotInventLifeFromAnError(t *testing.T) {
	lister := &fakeLister{checkErr: errors.New("connection refused")}
	if probeSession(context.Background(), lister, evolution.Instance{Token: "tok", Number: "55"}) {
		t.Fatal("a failed probe was read as a live session")
	}
}

// With no token or no number there is nothing to ask, and a probe that cannot
// be made must not count as one that failed meaningfully either.
func TestProbeSessionNeedsSomethingToAskWith(t *testing.T) {
	lister := &fakeLister{}
	for _, instance := range []evolution.Instance{
		{Token: "", Number: "55"},
		{Token: "tok", Number: ""},
	} {
		if probeSession(context.Background(), lister, instance) {
			t.Fatalf("probed with nothing to ask: %+v", instance)
		}
	}
	if lister.checked != 0 {
		t.Fatalf("reached WhatsApp %d times with nothing to ask", lister.checked)
	}
}
