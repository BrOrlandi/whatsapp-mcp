package health

import (
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
