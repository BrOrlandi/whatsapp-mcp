package health

import (
	"sync"
	"time"
)

type Snapshot struct {
	EvolutionConnected bool      `json:"evolution_connected"`
	LastEventAt        time.Time `json:"last_event_at,omitempty"`
	RabbitConnected    bool      `json:"rabbit_connected"`
	DatabaseConnected  bool      `json:"database_connected"`
}

const FreshnessWarning = "message freshness is not trustworthy; results may be incomplete"

func (s Snapshot) Stale(maxAge time.Duration) bool {
	if !s.EvolutionConnected || s.LastEventAt.IsZero() {
		return true
	}
	return time.Since(s.LastEventAt) > maxAge
}

func (s Snapshot) Ready(maxAge time.Duration) bool {
	return s.EvolutionConnected && !s.Stale(maxAge) && s.RabbitConnected && s.DatabaseConnected
}

type State struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewState() *State              { return &State{} }
func (s *State) Snapshot() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return s.snapshot }
func (s *State) SetEvolution(connected bool) {
	s.mu.Lock()
	s.snapshot.EvolutionConnected = connected
	s.mu.Unlock()
}
func (s *State) SetRabbit(connected bool) {
	s.mu.Lock()
	s.snapshot.RabbitConnected = connected
	s.mu.Unlock()
}
func (s *State) SetDatabase(connected bool) {
	s.mu.Lock()
	s.snapshot.DatabaseConnected = connected
	s.mu.Unlock()
}
func (s *State) MarkEvent(at time.Time) {
	s.mu.Lock()
	s.snapshot.LastEventAt = at.UTC()
	s.mu.Unlock()
}
func (s *State) SetDependencies(evolution, rabbit, database bool) {
	s.mu.Lock()
	s.snapshot.EvolutionConnected, s.snapshot.RabbitConnected, s.snapshot.DatabaseConnected = evolution, rabbit, database
	s.mu.Unlock()
}
