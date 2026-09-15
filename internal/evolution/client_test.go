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

// Evolution's listing carries each instance's token, which is what lets the
// panel adopt an instance it did not create rather than forcing a new pairing.
func TestFetchInstancesCarriesTheInstanceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"success","data":[{"id":"abc","name":"Pessoal","token":"inst-token","connected":true}]}`))
	}))
	defer server.Close()
	instances, err := New(server.URL, "global", time.Second).FetchInstances(context.Background())
	if err != nil || len(instances) != 1 || instances[0].Token != "inst-token" {
		t.Fatalf("instances=%+v err=%v", instances, err)
	}
	// The token is internal: it must never be serialised towards a client.
	encoded, err := json.Marshal(instances[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "inst-token") {
		t.Fatalf("the instance token leaked into JSON: %s", encoded)
	}
}
