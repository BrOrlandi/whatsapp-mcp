package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
)

func TestHealthIsLiveWhileReadinessReportsStale(t *testing.T) {
	state := health.NewState()
	state.SetDependencies(false, true, true)
	handler := Handler(state, time.Minute)
	for _, tc := range []struct {
		path string
		want int
	}{{"/healthz", http.StatusOK}, {"/readyz", http.StatusServiceUnavailable}} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != tc.want || !strings.Contains(recorder.Body.String(), "freshness is not trustworthy") {
			t.Fatalf("%s: %d %s", tc.path, recorder.Code, recorder.Body.String())
		}
	}
}
