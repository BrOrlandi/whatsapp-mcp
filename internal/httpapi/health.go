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

func writeHealth(w http.ResponseWriter, snapshot health.Snapshot, freshness time.Duration, live bool) {
	stale := snapshot.Stale(freshness)
	body := map[string]any{"evolution_connected": snapshot.EvolutionConnected, "last_event_at": snapshot.LastEventAt, "rabbit_connected": snapshot.RabbitConnected, "database_connected": snapshot.DatabaseConnected, "stale": stale}
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
