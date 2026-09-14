package evolution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusUsesAPIKeyAndAcceptsOnlineStates(t *testing.T) {
	for _, body := range []string{`{"state":"connected"}`, `{"instance":{"state":"open"}}`, `{"status":"online"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("apikey"); got != "secret" {
					t.Fatalf("apikey = %q", got)
				}
				if r.URL.Path != "/instance/status" {
					t.Fatalf("path = %q", r.URL.Path)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			client := New(server.URL, "secret", time.Second)
			connected, _, err := client.Status(context.Background())
			if err != nil || !connected {
				t.Fatalf("connected=%v err=%v", connected, err)
			}
		})
	}
}

func TestStatusRejectsUnknownState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"connecting"}`))
	}))
	defer server.Close()
	connected, state, err := New(server.URL, "key", time.Second).Status(context.Background())
	if err != nil || connected || state != "connecting" {
		t.Fatalf("connected=%v state=%q err=%v", connected, state, err)
	}
}

func TestFetchInstancesUsesAPIKeyAndDecodesInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/all" || r.Header.Get("apikey") != "secret" {
			t.Fatalf("request path=%q apikey=%q", r.URL.Path, r.Header.Get("apikey"))
		}
		_, _ = w.Write([]byte(`{"message":"success","data":[{"id":"abc","name":"Pessoal","jid":"5511999999999@s.whatsapp.net","connected":true},{"instance":{"instanceName":"Trabalho","instanceId":"def","state":"connecting"}}]}`))
	}))
	defer server.Close()
	instances, err := New(server.URL, "secret", time.Second).FetchInstances(context.Background())
	if err != nil || len(instances) != 2 || instances[0].Number != "5511999999999" || instances[0].Status != StatusConnected || instances[1].Status != StatusConnecting {
		t.Fatalf("instances=%+v err=%v", instances, err)
	}
}
