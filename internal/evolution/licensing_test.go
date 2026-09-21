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

// evolutionActivateRoute stands in for Evolution's GET /license/activate as it
// actually behaves: whatever arrives in ?code= is POSTed to the licensing
// server as an authorization_code before anything else, and the licensing
// server's refusal is passed straight back (pkg/core/c0.go:786-806). The old
// stub here answered "active" to anything, which is how a panel that handed
// over an api_key instead of a code passed its tests and failed against every
// real deployment.
func evolutionActivateRoute(t *testing.T, validCode string, seen *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/license/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "inactive", "instance_id": "inst-7"})
		case "/license/activate":
			code := r.URL.Query().Get("code")
			if seen != nil {
				*seen = code
			}
			if code != validCode {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"Exchange failed","details":"invalid authorization code"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "active"})
		default:
			t.Errorf("unexpected Evolution call %s", r.URL.Path)
		}
	}
}

func TestCompleteActivationGivesEvolutionTheCodeItself(t *testing.T) {
	var activatedWith string
	evo := httptest.NewServer(evolutionActivateRoute(t, "one-time", &activatedWith))
	defer evo.Close()
	// Nothing may reach the licensing server from here: the code is spent once,
	// and Evolution is the party that has to spend it.
	withLicensingStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the panel exchanged the code itself, at %s", r.URL.Path)
	})
	client := New(evo.URL, "key", time.Second)

	activation, err := client.CompleteActivation(context.Background(), "one-time")
	if err != nil {
		t.Fatalf("CompleteActivation: %v", err)
	}
	if activatedWith != "one-time" {
		t.Fatalf("Evolution was activated with %q, want the authorization code untouched", activatedWith)
	}
	if activation.InstanceID != "inst-7" {
		t.Fatalf("activation = %+v", activation)
	}
}

// The failure this replaced: the panel exchanged the code, kept the api_key and
// offered that to Evolution, which refused it because a key is not a code. The
// registration was completed upstream and no deployment could use it.
func TestCompleteActivationDoesNotOfferEvolutionAKey(t *testing.T) {
	var activatedWith string
	evo := httptest.NewServer(evolutionActivateRoute(t, "one-time", &activatedWith))
	defer evo.Close()
	withLicensingStub(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"api_key": "evo-key", "tier": "evolution-go", "customer_id": 42})
	})
	client := New(evo.URL, "key", time.Second)

	if _, err := client.CompleteActivation(context.Background(), "one-time"); err != nil {
		t.Fatalf("CompleteActivation: %v", err)
	}
	if activatedWith == "evo-key" {
		t.Fatal("the panel handed Evolution an api_key, which its activate route always refuses")
	}
}

func TestCompleteActivationRefusesACodeEvolutionRejects(t *testing.T) {
	evo := httptest.NewServer(evolutionActivateRoute(t, "one-time", nil))
	defer evo.Close()
	client := New(evo.URL, "key", time.Second)

	if _, err := client.CompleteActivation(context.Background(), "spent"); err == nil {
		t.Fatal("a rejected code activated a licence")
	}
}

// Reactivating from a key the panel kept cannot work through this route, and
// saying so is better than an error that looks like a transient outage: the
// only place Evolution takes a bare key is GLOBAL_API_KEY at startup.
func TestReactivateLicenseReportsThatEvolutionHasNowhereToPutAKey(t *testing.T) {
	evo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Evolution call %s", r.URL.Path)
	}))
	defer evo.Close()
	client := New(evo.URL, "key", time.Second)

	err := client.ReactivateLicense(context.Background(), "kept-key")
	if err == nil {
		t.Fatal("ReactivateLicense claimed to have reactivated a licence")
	}
	if !strings.Contains(err.Error(), "GLOBAL_API_KEY") {
		t.Fatalf("the error does not say where a key can go: %v", err)
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
