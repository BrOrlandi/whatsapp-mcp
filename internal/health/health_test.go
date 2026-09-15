package health

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshotIsStaleWhenEvolutionIsDisconnected(t *testing.T) {
	s := Snapshot{EvolutionConnected: false, LastEventAt: time.Now()}
	if !s.Stale(time.Minute) {
		t.Fatal("disconnected Evolution must make the snapshot stale")
	}
}

func TestSnapshotIsStaleWhenNoRecentEventExists(t *testing.T) {
	s := Snapshot{EvolutionConnected: true, LastEventAt: time.Now().Add(-2 * time.Minute)}
	if !s.Stale(time.Minute) {
		t.Fatal("an old event must make the snapshot stale")
	}
}

func TestSnapshotIsFreshWhenConnectedAndRecent(t *testing.T) {
	s := Snapshot{EvolutionConnected: true, LastEventAt: time.Now().Add(-10 * time.Second)}
	if s.Stale(time.Minute) {
		t.Fatal("a connected Evolution with a recent event must be fresh")
	}
}

func TestReadyRequiresEveryDependencyAndFreshEvents(t *testing.T) {
	s := Snapshot{EvolutionConnected: true, LastEventAt: time.Now(), RabbitConnected: true, DatabaseConnected: true}
	if !s.Ready(time.Minute) {
		t.Fatal("all healthy dependencies and a recent event must be ready")
	}
	s.RabbitConnected = false
	if s.Ready(time.Minute) {
		t.Fatal("a disconnected RabbitMQ must not be ready")
	}
}

// A poll may confirm a live session, but it must never overwrite a specific
// failure with a vague one: "logged out" tells the operator to scan a new QR
// code, while "disconnected" tells them to wait.
func TestReconcileNeverDowngradesASpecificFailure(t *testing.T) {
	state := NewState()
	state.SetWhatsApp("logged_out", "401", "", "")
	state.ReconcileWhatsApp(false)
	if got := state.Snapshot().WhatsApp; got.State != "logged_out" || got.Reason != "401" {
		t.Fatalf("specific failure was overwritten: %+v", got)
	}
	state.ReconcileWhatsApp(true)
	if got := state.Snapshot().WhatsApp; got.State != "connected" || got.Reason != "" {
		t.Fatalf("reconnect not recorded: %+v", got)
	}
	state.ReconcileWhatsApp(false)
	if got := state.Snapshot().WhatsApp; got.State != "disconnected" {
		t.Fatalf("drop from connected not recorded: %+v", got)
	}
}

// Before any event arrives the session state is unknown, which is different
// from knowing it is down.
func TestSessionStartsUnknown(t *testing.T) {
	if got := NewState().Snapshot().WhatsApp.State; got != StateUnknown {
		t.Fatalf("initial state = %q", got)
	}
}

func TestQueueCountersAndProblems(t *testing.T) {
	state := NewState()
	state.SetDependencies(true, true, true)
	state.SetQueueConsuming("message", true, "")
	state.MarkQueueEvent("message", time.Now(), false)
	state.MarkQueueEvent("message", time.Now(), true)
	state.SetQueueConsuming("historysync", false, "closed")

	snapshot := state.Snapshot()
	if len(snapshot.Queues) != 2 || snapshot.Queues[0].Name != "historysync" {
		t.Fatalf("queues = %+v", snapshot.Queues)
	}
	live := snapshot.Queues[1]
	if live.Delivered != 1 || live.Rejected != 1 {
		t.Fatalf("counters = %+v", live)
	}
	problems := snapshot.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0], "historysync") {
		t.Fatalf("problems = %v", problems)
	}
}
