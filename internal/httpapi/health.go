package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

func Handler(state *health.State, freshness time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, true) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, false) })
	return mux
}

// FullHandler mounts the probes, the remote MCP endpoint and the control panel
// on one server. The MCP endpoint authenticates every request on its own and
// shares nothing with the panel's session cookie.
func FullHandler(state *health.State, freshness time.Duration, web, remoteMCP http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, true) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, state.Snapshot(), freshness, false) })
	if remoteMCP != nil {
		mux.Handle("/mcp", remoteMCP)
	}
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
		// The version is here so that "which version is that box running" has an
		// answer a monitor can read, without a session on the panel.
		"version":             version.String(),
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
