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
	state.ObserveInstance(false, 0)
	if got := state.Snapshot().WhatsApp; got.State != "logged_out" || got.Reason != "401" {
		t.Fatalf("specific failure was overwritten: %+v", got)
	}
	state.ObserveInstance(true, 0)
	if got := state.Snapshot().WhatsApp; got.State != "connected" || got.Reason != "" {
		t.Fatalf("reconnect not recorded: %+v", got)
	}
	state.ObserveInstance(false, 0)
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

// The failure this guards happened in production: Evolution's instance record
// stayed false through a reconnect, a 15-second poll overwrote connection
// events from 13 seconds earlier, and the panel announced a disconnected
// WhatsApp while 200 messages a minute were being indexed from that very
// session. Messages arriving through a client prove the client is connected.
func TestAPollCannotDisconnectASessionThatIsReceiving(t *testing.T) {
	state := NewState()
	state.SetWhatsApp("connected", "", "5511999999999@s.whatsapp.net", "Fulano")
	state.MarkEvent(time.Now().Add(-20 * time.Second))

	if !state.ObserveInstance(false, 5*time.Minute) {
		t.Fatal("the poll was believed over the traffic it contradicts")
	}
	snapshot := state.Snapshot()
	if snapshot.WhatsApp.State != "connected" {
		t.Fatalf("session reported %q while its messages were arriving", snapshot.WhatsApp.State)
	}
	if !snapshot.EvolutionConnected {
		t.Fatal("readiness was withdrawn from a session that is demonstrably up")
	}
}

// Silence is not proof. With nothing arriving there is no evidence to weigh
// against the poll, and the poll is all there is.
func TestAPollIsBelievedWhenNothingIsArriving(t *testing.T) {
	state := NewState()
	state.SetWhatsApp("connected", "", "", "")
	state.MarkEvent(time.Now().Add(-30 * time.Minute))

	if state.ObserveInstance(false, 5*time.Minute) {
		t.Fatal("stale traffic was treated as proof of a live session")
	}
	if got := state.Snapshot().WhatsApp.State; got != "disconnected" {
		t.Fatalf("session reported %q, want disconnected", got)
	}
}

// An explicit event outranks everything here, including live traffic: a
// logged-out session can still have events in flight, and telling the operator
// to wait instead of to scan a QR code would strand them.
func TestTrafficDoesNotArgueWithAnExplicitFailure(t *testing.T) {
	state := NewState()
	state.MarkEvent(time.Now())
	state.SetWhatsApp("logged_out", "401", "", "")

	if state.ObserveInstance(false, 5*time.Minute) {
		t.Fatal("the veto overrode an explicit failure event")
	}
	if got := state.Snapshot().WhatsApp.State; got != "logged_out" {
		t.Fatalf("session reported %q, want logged_out", got)
	}
}
