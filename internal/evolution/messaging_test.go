package evolution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Evolution Go answers a send with whatsmeow's SendResponse, whose id sits
// under Info. Reading only the flat fields lost it on every send, and the id is
// what /message/status is keyed by: without it there is no way to ask whether
// the message arrived.
func TestSendTextReadsTheNestedAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"Info":{"Chat":"55@s.whatsapp.net","ID":"3EB035789D5041DB3388CB","ServerID":0,"Timestamp":"2026-09-15T00:54:40.847487677-03:00"},"Message":{"extendedTextMessage":{"text":"oi"}}},"message":"success"}`))
	}))
	defer server.Close()

	sent, err := New(server.URL, "global", time.Second).SendText(context.Background(), "tok", "55@s.whatsapp.net", "oi")
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if sent.ID != "3EB035789D5041DB3388CB" {
		t.Fatalf("id = %q, want the one nested under Info", sent.ID)
	}
	if sent.Timestamp.IsZero() {
		t.Fatal("the nested timestamp was not read")
	}
}

// A message that was never encrypted for the recipient has no delivery record
// at all, and that emptiness is the only signal separating it from one that
// arrived. Reporting it as a status would erase the distinction.
func TestDeliveredSeparatesArrivalFromSilence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status string
	}{
		{"arrived", `{"data":{"result":{"message_id":"ABC","timestamp":"2026-09-15 00:54:41","status":"Delivered"}},"message":"success"}`, "Delivered"},
		{"never left", `{"data":{"result":null},"message":"success"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			delivery, err := New(server.URL, "global", time.Second).Delivered(context.Background(), "tok", "ABC")
			if err != nil {
				t.Fatalf("status failed: %v", err)
			}
			if delivery.Status != tc.status {
				t.Fatalf("status = %q, want %q", delivery.Status, tc.status)
			}
		})
	}
}
