package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
)

func Handler(state *health.State, freshness time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, true) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, false) })
	return mux
}

func FullHandler(state *health.State, freshness time.Duration, web http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, true) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, false) })
	mux.Handle("/", web)
	return mux
}

// writeHealth reports the whole snapshot, including the WhatsApp session state
// and the per-queue consumption, so a probe failure can be diagnosed from the
// response alone. It carries no credentials: every field here is state, never
// configuration.
func writeHealth(w http.ResponseWriter, snapshot health.Snapshot, freshness time.Duration, live bool) {
	stale := snapshot.Stale(freshness)
	body := map[string]any{
		"evolution_connected": snapshot.EvolutionConnected,
		"last_event_at":       snapshot.LastEventAt,
		"last_message_at":     snapshot.LastMessageAt,
		"rabbit_connected":    snapshot.RabbitConnected,
		"database_connected":  snapshot.DatabaseConnected,
		"whatsapp":            snapshot.WhatsApp,
		"queues":              snapshot.Queues,
		"stale":               stale,
	}
	if problems := snapshot.Problems(); len(problems) > 0 {
		body["problems"] = problems
	}
	if stale {
		body["warning"] = health.FreshnessWarning
	}
	status := http.StatusOK
	if !live && !snapshot.Ready(freshness) {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
