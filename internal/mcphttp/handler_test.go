package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeIndex struct{ tokens map[string]string }

func (f fakeIndex) SelectedInstance(context.Context) (string, error) { return "", nil }
func (f fakeIndex) ManagedInstances(context.Context) (map[string]string, error) {
	return map[string]string{"inst-1": "pessoal"}, nil
}
func (f fakeIndex) InstanceToken(_ context.Context, id string) (string, error) {
	token, ok := f.tokens[id]
	if !ok {
		return "", errors.New("unknown instance")
	}
	return token, nil
}
func (f fakeIndex) Coverage(context.Context, string) (store.Coverage, error) {
	return store.Coverage{}, nil
}
func (f fakeIndex) ListChats(context.Context, string, string, int) ([]store.Chat, error) {
	return nil, nil
}
func (f fakeIndex) Messages(context.Context, string, store.MessageQuery) ([]store.Message, error) {
	return nil, nil
}
func (f fakeIndex) OldestMessage(context.Context, string, string) (store.Message, error) {
	return store.Message{}, errors.New("empty")
}
func (f fakeIndex) RawMessage(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("empty")
}

type fakeLive struct{ tokens []string }

func (f *fakeLive) Contacts(_ context.Context, token string) ([]evolution.Contact, error) {
	f.tokens = append(f.tokens, token)
	return []evolution.Contact{{JID: "a@s.whatsapp.net", Name: "Alguém"}}, nil
}
func (f *fakeLive) Groups(context.Context, string) ([]evolution.Group, error) { return nil, nil }
func (f *fakeLive) Group(context.Context, string, string) (evolution.Group, error) {
	return evolution.Group{}, nil
}
func (f *fakeLive) SendText(context.Context, string, string, string) (evolution.SentMessage, error) {
	return evolution.SentMessage{}, nil
}
func (f *fakeLive) SendMedia(context.Context, string, string, string, string, string, string) (evolution.SentMessage, error) {
	return evolution.SentMessage{}, nil
}
func (f *fakeLive) DownloadMedia(context.Context, string, json.RawMessage) (evolution.Media, error) {
	return evolution.Media{}, nil
}
func (f *fakeLive) RequestHistory(context.Context, string, evolution.Anchor, int) error { return nil }

type fakeAuth struct {
	valid    string
	instance string
	calls    int
}

func (f *fakeAuth) Authenticate(_ context.Context, secret string) (string, error) {
	f.calls++
	if secret != f.valid {
		return "", errors.New("unknown key")
	}
	return f.instance, nil
}

func newServer(t *testing.T, auth Authenticator, live *fakeLive) *httptest.Server {
	t.Helper()
	state := health.NewState()
	state.SetDependencies(true, true, true)
	state.MarkEvent(time.Now())
	server := mcp.New(fakeIndex{tokens: map[string]string{"inst-1": "tok-1"}}, live, state, time.Minute)
	ts := httptest.NewServer(New(server, auth, nil))
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, url, key, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The endpoint has no anonymous mode. A request without a credential, or with a
// rejected one, is refused before any tool runs.
func TestEndpointRefusesAnonymousAndRejectedCredentials(t *testing.T) {
	live := &fakeLive{}
	auth := &fakeAuth{valid: "wamcp-good", instance: "inst-1"}
	ts := newServer(t, auth, live)
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

	for _, key := range []string{"", "wamcp-wrong"} {
		resp := post(t, ts.URL, key, initialize)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("key %q got status %d", key, resp.StatusCode)
		}
		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Error("401 carried no WWW-Authenticate header")
		}
		// The answer must not distinguish a key that never existed from one that
		// was revoked, and must never echo the credential back.
		if strings.Contains(string(body), "revoked") || strings.Contains(string(body), key) && key != "" {
			t.Fatalf("response leaked credential detail: %s", body)
		}
	}
	if len(live.tokens) != 0 {
		t.Fatal("a rejected request reached WhatsApp")
	}
}

// A valid credential resolves to its instance, and the tool runs against that
// instance's token.
func TestValidCredentialRunsToolsForItsInstance(t *testing.T) {
	live := &fakeLive{}
	ts := newServer(t, &fakeAuth{valid: "wamcp-good", instance: "inst-1"}, live)

	resp := post(t, ts.URL, "wamcp-good", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_contacts","arguments":{}}}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if len(live.tokens) != 1 || live.tokens[0] != "tok-1" {
		t.Fatalf("tokens = %v", live.tokens)
	}
	if !strings.Contains(string(body), "Alguém") {
		t.Fatalf("body = %s", body)
	}
}

// Each request authenticates on its own, which is what makes a dropped
// connection cost nothing.
func TestEveryRequestIsAuthenticatedIndependently(t *testing.T) {
	auth := &fakeAuth{valid: "wamcp-good", instance: "inst-1"}
	ts := newServer(t, auth, &fakeLive{})
	for i := 0; i < 3; i++ {
		resp := post(t, ts.URL, "wamcp-good", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d got %d", i, resp.StatusCode)
		}
	}
	if auth.calls != 3 {
		t.Fatalf("authenticated %d times for 3 requests", auth.calls)
	}
}

// A credential in a query string ends up in proxy and browser logs, so only the
// Authorization header is accepted.
func TestCredentialIsNotAcceptedFromTheQueryString(t *testing.T) {
	ts := newServer(t, &fakeAuth{valid: "wamcp-good", instance: "inst-1"}, &fakeLive{})
	resp := post(t, ts.URL+"?api_key=wamcp-good&token=wamcp-good", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// A notification has no reply; Streamable HTTP expects an empty 202.
func TestNotificationsGetAnAcceptedResponse(t *testing.T) {
	ts := newServer(t, &fakeAuth{valid: "wamcp-good", instance: "inst-1"}, &fakeLive{})
	resp := post(t, ts.URL, "wamcp-good", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || len(body) != 0 {
		t.Fatalf("status = %d body = %q", resp.StatusCode, body)
	}
}

func TestOnlyPostIsAccepted(t *testing.T) {
	ts := newServer(t, &fakeAuth{valid: "wamcp-good", instance: "inst-1"}, &fakeLive{})
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
		t.Fatalf("status = %d allow = %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
}

// An oversized body is refused rather than buffered.
func TestOversizedBodyIsRefused(t *testing.T) {
	ts := newServer(t, &fakeAuth{valid: "wamcp-good", instance: "inst-1"}, &fakeLive{})
	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", maxBody+1024) + `"}}`
	resp := post(t, ts.URL, "wamcp-good", huge)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// Guessing is throttled per source address, so an endpoint that cannot
// realistically be brute-forced does not invite the attempt either.
func TestRepeatedFailuresAreThrottled(t *testing.T) {
	auth := &fakeAuth{valid: "wamcp-good", instance: "inst-1"}
	ts := newServer(t, auth, &fakeLive{})
	const probe = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	throttled := false
	for i := 0; i < maxFailures+3; i++ {
		resp := post(t, ts.URL, "wamcp-bad", probe)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			throttled = true
			if resp.Header.Get("Retry-After") == "" {
				t.Error("throttled response carried no Retry-After")
			}
			break
		}
	}
	if !throttled {
		t.Fatal("repeated failures were never throttled")
	}
}
