package evolution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

	sent, err := New(server.URL, "global", time.Second).SendText(context.Background(), "tok", "55@s.whatsapp.net", "oi", SendOptions{})
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

// Evolution answers /user/check with {"data":{"Users":[…]}} and the field is
// IsInWhatsapp. This asked for a bare array of {Query, JID, IsIn}, so every
// call failed on the decode: the tool reported a WhatsApp error for a query
// WhatsApp had answered correctly, which is the worst shape a bug can take —
// it blames the wrong component. Captured from the live server.
func TestCheckNumbersReadsEvolutionsActualShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/check" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"Users":[
			{"Query":"+5511923456789","IsInWhatsapp":true,"JID":"5511923456789@s.whatsapp.net","RemoteJID":"5511923456789@s.whatsapp.net","LID":"100000000000001@lid","VerifiedName":""},
			{"Query":"+5500000000000","IsInWhatsapp":false,"JID":"","RemoteJID":"","LID":"","VerifiedName":""}
		]},"message":"success"}`))
	}))
	defer server.Close()

	found, err := New(server.URL, "global", time.Second).CheckNumbers(context.Background(), "tok", []string{"5511923456789", "5500000000000"})
	if err != nil {
		t.Fatalf("CheckNumbers failed on the shape the server actually sends: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("got %d results, want 2", len(found))
	}
	if !found[0].OnWhatsApp || found[0].JID != "5511923456789@s.whatsapp.net" || found[0].Number != "+5511923456789" {
		t.Fatalf("first result = %+v", found[0])
	}
	// A number with no account has to stay negative: a caller uses this to
	// decide whether sending is even possible.
	if found[1].OnWhatsApp || found[1].JID != "" {
		t.Fatalf("a number with no account was reported as reachable: %+v", found[1])
	}
}

// WhatsApp discards media some time after it is sent, and Evolution reports
// that as a 500 wrapping the CDN's 404. That is named, so nobody retries a
// download that cannot succeed or mistakes it for the server being down.
func TestDownloadMediaNamesExpiredMedia(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to download media: download failed with status code 404"}`))
	}))
	defer server.Close()
	_, err := New(server.URL, "global", time.Second).DownloadMedia(context.Background(), "tok", []byte(`{"audioMessage":{}}`))
	if !errors.Is(err, ErrMediaExpired) {
		t.Fatalf("err = %v, want ErrMediaExpired", err)
	}
}

// A reply, a mention and a forward travel in the fields Evolution reads:
// quoted, mentionedJid and forwardingScore. A sticker goes to its own route.
func TestSendOptionsReachEvolution(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"data":{"Info":{"ID":"X"}}}`))
	}))
	defer server.Close()
	client := New(server.URL, "global", time.Second)
	opts := SendOptions{QuotedID: "Q1", QuotedParticipant: "551@s.whatsapp.net", Mentions: []string{"552@s.whatsapp.net"}, Forwarded: true}
	if _, err := client.SendText(context.Background(), "tok", "120@g.us", "oi @552", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendMedia(context.Background(), "tok", "120@g.us", "sticker", "https://x.example/s.webp", "", "", opts); err != nil {
		t.Fatal(err)
	}
	text := bodies[0]
	quoted, _ := text["quoted"].(map[string]any)
	if paths[0] != "/send/text" || quoted["messageId"] != "Q1" || quoted["participant"] != "551@s.whatsapp.net" || text["forwardingScore"] != float64(1) {
		t.Fatalf("text body = %v", text)
	}
	if mentions, _ := text["mentionedJid"].([]any); len(mentions) != 1 || mentions[0] != "552@s.whatsapp.net" {
		t.Fatalf("mentions = %v", text["mentionedJid"])
	}
	if paths[1] != "/send/sticker" || bodies[1]["sticker"] != "https://x.example/s.webp" || bodies[1]["forwardingScore"] != nil {
		t.Fatalf("sticker = %s %v", paths[1], bodies[1])
	}
}

func TestGroupAndPresenceRoutes(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.URL.Path+" "+string(body))
		if r.URL.Path == "/group/invitelink" {
			_, _ = w.Write([]byte(`{"message":"success","data":"https://chat.whatsapp.com/ABC"}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":"success"}`))
	}))
	defer server.Close()
	c := New(server.URL, "global", time.Second)
	ctx := context.Background()
	if err := c.UpdateParticipants(ctx, "tok", "120@g.us", "promote", []string{"551@s.whatsapp.net"}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateParticipants(ctx, "tok", "120@g.us", "kick", []string{"551"}); err == nil {
		t.Fatal("an unknown action must be refused before Evolution is called")
	}
	link, err := c.GroupInviteLink(ctx, "tok", "120@g.us", true)
	if err != nil || link != "https://chat.whatsapp.com/ABC" {
		t.Fatalf("link = %q, %v", link, err)
	}
	if err := c.Presence(ctx, "tok", "551@s.whatsapp.net", true, true); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkRead(ctx, "tok", "551@s.whatsapp.net", []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`/group/participant {"action":"promote","groupJid":"120@g.us","participants":["551@s.whatsapp.net"]}`,
		`/group/invitelink {"groupJid":"120@g.us","reset":true}`,
		`/message/presence {"isAudio":true,"number":"551@s.whatsapp.net","state":"composing"}`,
		`/message/markread {"id":["A","B"],"number":"551@s.whatsapp.net"}`,
	}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("call %d = %q, want %q", i, got, w)
		}
	}
}
