package evolution

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
)

// Evolution resolves the target instance from the key on the request, so the
// panel must send the global key only for administrative routes and the
// instance token for everything else. Getting this backwards silently operates
// the wrong instance.
func TestInstanceCallsUseTheRightKey(t *testing.T) {
	type seen struct{ method, path, key, body string }
	var got seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = seen{r.Method, r.URL.Path, r.Header.Get("apikey"), string(body)}
		_, _ = w.Write([]byte(`{"message":"success","data":{"id":"abc","name":"pessoal"}}`))
	}))
	defer server.Close()
	client := New(server.URL, "global-key", time.Second)
	ctx := context.Background()

	if _, err := client.CreateInstance(ctx, "pessoal", "inst-token"); err != nil {
		t.Fatal(err)
	}
	if got.path != "/instance/create" || got.key != "global-key" {
		t.Fatalf("create: %+v", got)
	}
	if !strings.Contains(got.body, `"token":"inst-token"`) || !strings.Contains(got.body, `"name":"pessoal"`) {
		t.Fatalf("create body: %s", got.body)
	}

	if err := client.DeleteInstance(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete || got.path != "/instance/delete/abc" || got.key != "global-key" {
		t.Fatalf("delete: %+v", got)
	}

	for _, action := range []struct {
		name string
		run  func() error
		path string
	}{
		{"connect", func() error { return client.ConnectInstance(ctx, "inst-token") }, "/instance/connect"},
		{"disconnect", func() error { return client.DisconnectInstance(ctx, "inst-token") }, "/instance/disconnect"},
		{"logout", func() error { return client.LogoutInstance(ctx, "inst-token") }, "/instance/logout"},
	} {
		if err := action.run(); err != nil {
			t.Fatalf("%s: %v", action.name, err)
		}
		if got.path != action.path || got.key != "inst-token" {
			t.Fatalf("%s: %+v", action.name, got)
		}
	}
}

// Connecting is also what wires the instance to RabbitMQ: Evolution publishes
// nothing unless rabbitmqEnable says so, and it delivers only the subscribed
// events.
func TestConnectSubscribesToTheIngestedEvents(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))
	defer server.Close()
	if err := New(server.URL, "global", time.Second).ConnectInstance(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if body["rabbitmqEnable"] != "global" {
		t.Fatalf("rabbitmqEnable = %v", body["rabbitmqEnable"])
	}
	subscribed, _ := body["subscribe"].([]any)
	if len(subscribed) != len(IngestedEvents) {
		t.Fatalf("subscribe = %v", subscribed)
	}
	for i, want := range IngestedEvents {
		if subscribed[i] != want {
			t.Fatalf("subscribe[%d] = %v, want %s", i, subscribed[i], want)
		}
	}
}

func TestQRCodeReturnsInlineImageAndCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/qr" || r.Header.Get("apikey") != "tok" {
			t.Fatalf("path=%q key=%q", r.URL.Path, r.Header.Get("apikey"))
		}
		_, _ = w.Write([]byte(`{"message":"success","data":{"qrcode":"data:image/png;base64,AAAA","code":"2@abc"}}`))
	}))
	defer server.Close()
	code, err := New(server.URL, "global", time.Second).QRCode(context.Background(), "tok")
	if err != nil || code.Image != "data:image/png;base64,AAAA" || code.Code != "2@abc" {
		t.Fatalf("code=%+v err=%v", code, err)
	}
}

// A refusal must carry Evolution's own wording, otherwise the panel can only
// show a bare status code the operator cannot act on.
func TestFailedCallReportsEvolutionReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"instance not found"}`))
	}))
	defer server.Close()
	_, err := New(server.URL, "global", time.Second).QRCode(context.Background(), "tok")
	if err == nil || !strings.Contains(err.Error(), "instance not found") {
		t.Fatalf("err = %v", err)
	}
}

// An instance that is already paired has no QR to show. The panel needs that
// distinguished from a real failure so it can wait for the connection instead
// of reporting an error.
func TestQRCodeReportsAlreadyPairedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"session already logged in"}`))
	}))
	defer server.Close()
	_, err := New(server.URL, "global", time.Second).QRCode(context.Background(), "tok")
	if !errors.Is(err, ErrLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}
