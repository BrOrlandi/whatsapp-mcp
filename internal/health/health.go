// Package health holds the live picture of the gateway: whether its
// dependencies answer, what WhatsApp itself reports, and how each RabbitMQ
// queue is being consumed. Everything the panel shows and every status the MCP
// tools report comes from here.
package health

import (
	"sort"
	"sync"
	"time"
)

// WhatsApp is the session state as reported by Evolution's own connection
// events, which is more truthful and cheaper than polling the HTTP API.
type WhatsApp struct {
	State     string    `json:"state"`
	Reason    string    `json:"reason,omitempty"`
	JID       string    `json:"jid,omitempty"`
	PushName  string    `json:"push_name,omitempty"`
	ChangedAt time.Time `json:"changed_at,omitempty"`
}

// Queue is what is known about one ingestion queue. A queue that is declared by
// Evolution but never consumed grows without bound, so "consuming" is a fact
// worth surfacing rather than assuming.
type Queue struct {
	Name        string    `json:"name"`
	Consuming   bool      `json:"consuming"`
	Delivered   int64     `json:"delivered"`
	Rejected    int64     `json:"rejected"`
	LastEventAt time.Time `json:"last_event_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

// Reprojection is the state of the self-healing pass that rebuilds the message
// index from the stored payloads after a decoder fix.
type Reprojection struct {
	Running    bool      `json:"running"`
	Events     int       `json:"events,omitempty"`
	Messages   int       `json:"messages,omitempty"`
	Orphans    int64     `json:"orphans_remaining,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type Snapshot struct {
	EvolutionConnected bool         `json:"evolution_connected"`
	LastEventAt        time.Time    `json:"last_event_at,omitempty"`
	LastMessageAt      time.Time    `json:"last_message_at,omitempty"`
	LastHistoryAt      time.Time    `json:"last_history_at,omitempty"`
	RabbitConnected    bool         `json:"rabbit_connected"`
	DatabaseConnected  bool         `json:"database_connected"`
	WhatsApp           WhatsApp     `json:"whatsapp"`
	Queues             []Queue      `json:"queues,omitempty"`
	Reprojection       Reprojection `json:"reprojection,omitzero"`
}

const FreshnessWarning = "message freshness is not trustworthy; results may be incomplete"

// StateUnknown is reported before any connection event has arrived, which is
// different from knowing the session is down.
const StateUnknown = "unknown"

// Stale reports that the index may be missing recent messages. A quiet account
// is indistinguishable from a broken pipeline by age alone, so this stays a
// warning the caller weighs rather than a verdict.
func (s Snapshot) Stale(maxAge time.Duration) bool {
	if !s.EvolutionConnected || s.LastEventAt.IsZero() {
		return true
	}
	return time.Since(s.LastEventAt) > maxAge
}

func (s Snapshot) Ready(maxAge time.Duration) bool {
	return s.EvolutionConnected && !s.Stale(maxAge) && s.RabbitConnected && s.DatabaseConnected
}

// Problems lists what is wrong in plain terms, so the panel and the MCP tools
// describe the same failure instead of each inventing its own wording.
func (s Snapshot) Problems() []string {
	var problems []string
	if !s.DatabaseConnected {
		problems = append(problems, "o banco de dados do gateway está inacessível")
	}
	if !s.RabbitConnected {
		problems = append(problems, "a fila de eventos está inacessível, então nenhuma mensagem nova é indexada")
	}
	switch s.WhatsApp.State {
	case "logged_out":
		problems = append(problems, "a sessão do WhatsApp foi encerrada e exige um novo QR code")
	case "banned":
		problems = append(problems, "a conta do WhatsApp está temporariamente banida")
	case "failed":
		problems = append(problems, "a conexão com o WhatsApp falhou")
	case "disconnected":
		problems = append(problems, "o WhatsApp está desconectado")
	}
	if s.WhatsApp.Reason != "" {
		problems = append(problems, "motivo informado: "+s.WhatsApp.Reason)
	}
	if s.Reprojection.Running {
		problems = append(problems, "o índice está sendo reconstruído a partir dos eventos guardados; mensagens podem faltar até terminar")
	}
	if s.Reprojection.Orphans > 0 && !s.Reprojection.Running {
		problems = append(problems, "ainda há mensagens gravadas que nenhuma consulta alcança; a reconstrução não as recuperou")
	}
	for _, queue := range s.Queues {
		if !queue.Consuming {
			problems = append(problems, "a fila "+queue.Name+" não está sendo consumida")
		}
	}
	return problems
}

type State struct {
	mu       sync.RWMutex
	snapshot Snapshot
	queues   map[string]Queue
}

func NewState() *State { return &State{queues: map[string]Queue{}} }

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.snapshot
	snapshot.Queues = make([]Queue, 0, len(s.queues))
	for _, queue := range s.queues {
		snapshot.Queues = append(snapshot.Queues, queue)
	}
	sort.Slice(snapshot.Queues, func(i, j int) bool { return snapshot.Queues[i].Name < snapshot.Queues[j].Name })
	if snapshot.WhatsApp.State == "" {
		snapshot.WhatsApp.State = StateUnknown
	}
	return snapshot
}

// SetEvolution records that Evolution itself could not be asked. It is the
// unknown case rather than a verdict about the session, and it is separate
// from ObserveInstance because a failed poll carries no evidence at all.
func (s *State) SetEvolution(connected bool) {
	s.mu.Lock()
	s.snapshot.EvolutionConnected = connected
	s.mu.Unlock()
}

// ObserveInstance records what a poll of Evolution's own instance record said,
// and is the only place a poll is allowed to decide the session is down.
//
// The two signals disagree, and not symmetrically. Evolution's AMQP connection
// events come from the WhatsApp client itself; its instance record is a row it
// updates alongside, and that row has been seen to stay false through a
// reconnect — the client talking to WhatsApp the whole time. A 15-second poll
// then overwrote connection events from 13 seconds earlier, and the panel
// announced a disconnected session while 200 messages a minute were being
// indexed from it.
//
// So a poll may confirm a session and may report one that was already down,
// but it may not take down a session that is currently receiving: messages
// arriving through a client are proof that the client is connected, and no row
// in a database outranks that. trafficWindow is how recent an event has to be
// to count as proof.
//
// It reports whether it refused the poll, which is worth a log line: the
// contradiction means Evolution's record is wrong and will stay wrong until
// something reconnects the instance.
func (s *State) ObserveInstance(connected bool, trafficWindow time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	refused := false
	// Only a downgrade is refused, and only from "connected". An explicit
	// disconnected, logged_out or banned event is authoritative — those arrive
	// from the client too, and this must never argue with them.
	if !connected && s.snapshot.WhatsApp.State == "connected" && s.receiving(trafficWindow) {
		connected, refused = true, true
	}
	s.snapshot.EvolutionConnected = connected
	current := &s.snapshot.WhatsApp
	switch {
	case connected:
		if current.State != "connected" {
			current.ChangedAt = time.Now().UTC()
		}
		current.State, current.Reason = "connected", ""
	case current.State == "" || current.State == "connected":
		current.State = "disconnected"
		current.ChangedAt = time.Now().UTC()
	}
	return refused
}

// receiving reports whether an event has arrived recently enough to prove the
// WhatsApp client is alive. Callers hold the lock.
func (s *State) receiving(window time.Duration) bool {
	if s.snapshot.LastEventAt.IsZero() || window <= 0 {
		return false
	}
	return time.Since(s.snapshot.LastEventAt) <= window
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

// MarkMessage and MarkHistory separate a live message from a backfilled one, so
// a long history sync is never mistaken for live traffic.
func (s *State) MarkMessage(at time.Time) {
	s.mu.Lock()
	s.snapshot.LastMessageAt = at.UTC()
	s.mu.Unlock()
}
func (s *State) MarkHistory(at time.Time) {
	s.mu.Lock()
	s.snapshot.LastHistoryAt = at.UTC()
	s.mu.Unlock()
}

// SetWhatsApp records what a connection event said about the session.
func (s *State) SetWhatsApp(state, reason, jid, pushName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := &s.snapshot.WhatsApp
	if current.State != state {
		current.ChangedAt = time.Now().UTC()
	}
	current.State, current.Reason = state, reason
	if jid != "" {
		current.JID = jid
	}
	if pushName != "" {
		current.PushName = pushName
	}
	if state == "connected" {
		current.Reason = ""
	}
}

// ReconcileWhatsApp folds a polled connection check into the session state.
// The poll is ground truth for "connected", which is why it may always set it;
// it is not allowed to overwrite a specific failure such as a logged-out or
// banned session with a vague "disconnected", because that would erase the only
// explanation the operator has.
// SetReprojection records what the repair pass is doing, so a long rebuild is
// visible rather than looking like a stall.
func (s *State) SetReprojection(running bool, events, messages int, orphans int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Reprojection = Reprojection{Running: running, Events: events, Messages: messages, Orphans: orphans}
	if !running {
		s.snapshot.Reprojection.FinishedAt = time.Now().UTC()
	}
}

func (s *State) SetQueueConsuming(name string, consuming bool, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.queues[name]
	queue.Name, queue.Consuming, queue.LastError = name, consuming, lastError
	s.queues[name] = queue
}

func (s *State) MarkQueueEvent(name string, at time.Time, rejected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.queues[name]
	queue.Name, queue.LastEventAt = name, at.UTC()
	if rejected {
		queue.Rejected++
	} else {
		queue.Delivered++
	}
	s.queues[name] = queue
}

func (s *State) SetDependencies(evolution, rabbit, database bool) {
	s.mu.Lock()
	s.snapshot.EvolutionConnected, s.snapshot.RabbitConnected, s.snapshot.DatabaseConnected = evolution, rabbit, database
	s.mu.Unlock()
}
