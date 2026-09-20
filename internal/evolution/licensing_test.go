package evolution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withLicensingStub points the licensing client at a stub server, restoring the
// real address for whichever test runs next.
func withLicensingStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := licensingServer
	licensingServer = server.URL
	t.Cleanup(func() {
		licensingServer = previous
		server.Close()
	})
	return server
}

func TestRegisterOperatorDrivesTheWholeMagicLink(t *testing.T) {
	var seenMagicLink map[string]string
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/license/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "inactive", "instance_id": "inst-7"})
		case "/license/register":
			// Evolution only registers the redirect on the call that generates
			// the token, which is why every call carries it.
			if got := r.URL.Query().Get("redirect_uri"); got != "https://panel.example/instancias/licenca/retorno" {
				t.Errorf("redirect_uri = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"register_url": licensingServer + "/register?token=tok-1",
				"status":       "pending",
			})
		default:
			t.Errorf("unexpected Evolution call %s", r.URL.Path)
		}
	}))
	defer evo.Close()
	withLicensingStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/magic-link" {
			t.Errorf("licensing call %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenMagicLink); err != nil {
			t.Errorf("decoding magic-link payload: %v", err)
		}
	})
	client := New(evo.URL, "key", time.Second)

	if err := client.RegisterOperator(context.Background(), "b@example.com", "B", "https://panel.example/instancias/licenca/retorno"); err != nil {
		t.Fatalf("RegisterOperator: %v", err)
	}
	if seenMagicLink["token"] != "tok-1" || seenMagicLink["email"] != "b@example.com" || seenMagicLink["name"] != "B" {
		t.Fatalf("magic-link payload = %v", seenMagicLink)
	}
}

func TestCompleteActivationExchangesTheCodeAndActivatesWithTheKey(t *testing.T) {
	var activatedWith string
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/license/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "inactive", "instance_id": "inst-7"})
		case "/license/activate":
			activatedWith = r.URL.Query().Get("code")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "active"})
		default:
			t.Errorf("unexpected Evolution call %s", r.URL.Path)
		}
	}))
	defer evo.Close()
	withLicensingStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/register/exchange" {
			t.Errorf("licensing call %s", r.URL.Path)
		}
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["authorization_code"] != "one-time" || payload["instance_id"] != "inst-7" {
			t.Errorf("exchange payload = %v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"api_key": "evo-key", "tier": "evolution-go", "customer_id": 42})
	})
	client := New(evo.URL, "key", time.Second)

	activation, err := client.CompleteActivation(context.Background(), "one-time")
	if err != nil {
		t.Fatalf("CompleteActivation: %v", err)
	}
	if activation.APIKey != "evo-key" || activation.Tier != "evolution-go" || activation.CustomerID != 42 || activation.InstanceID != "inst-7" {
		t.Fatalf("activation = %+v", activation)
	}
	if activatedWith != "evo-key" {
		t.Fatalf("Evolution was activated with %q, want the exchanged key", activatedWith)
	}
}

func TestCompleteActivationRefusesACodeTheServerRejects(t *testing.T) {
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "inactive", "instance_id": "inst-7"})
	}))
	defer evo.Close()
	withLicensingStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"code already used"}`))
	})
	client := New(evo.URL, "key", time.Second)

	if _, err := client.CompleteActivation(context.Background(), "spent"); err == nil {
		t.Fatal("a rejected code activated a licence")
	}
}

func TestReactivateLicenseHandEvolutionTheKeyItKept(t *testing.T) {
	var activatedWith string
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/license/activate" {
			t.Errorf("unexpected Evolution call %s", r.URL.Path)
		}
		activatedWith = r.URL.Query().Get("code")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "active"})
	}))
	defer evo.Close()
	client := New(evo.URL, "key", time.Second)

	if err := client.ReactivateLicense(context.Background(), "kept-key"); err != nil {
		t.Fatalf("ReactivateLicense: %v", err)
	}
	if activatedWith != "kept-key" {
		t.Fatalf("activated with %q", activatedWith)
	}
}

func TestRegisterOperatorNeedsATokenInTheRegisterURL(t *testing.T) {
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"register_url": licensingServer + "/register?notatoken=1", "status": "pending"})
	}))
	defer evo.Close()
	client := New(evo.URL, "key", time.Second)

	err := client.RegisterOperator(context.Background(), "b@example.com", "B", "")
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("a register URL without a token was accepted: %v", err)
	}
}
