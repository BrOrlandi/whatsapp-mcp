package events

import (
	"encoding/json"
	"testing"
	"time"
)

// Evolution Go wraps whatsmeow's own structs, so a live message arrives with Go
// field names under Info. The previous decoder expected the Evolution API v2
// shape and silently produced empty instance, chat and message ids.
func TestDecodeLiveMessageUsesWhatsmeowFieldNames(t *testing.T) {
	raw := []byte(`{
      "event":"Message",
      "instanceId":"inst-1",
      "instanceName":"pessoal",
      "data":{
        "Info":{
          "ID":"3EB0ABC",
          "Chat":"5511999999999@s.whatsapp.net",
          "Sender":"5511888888888@s.whatsapp.net",
          "IsFromMe":false,
          "IsGroup":false,
          "PushName":"Fulano",
          "Timestamp":"2026-09-14T10:00:00Z"
        },
        "Message":{"conversation":"bom dia"}
      }}`)
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != KindMessage {
		t.Fatalf("kind = %q", decoded.Kind)
	}
	if decoded.Record.InstanceID != "inst-1" {
		t.Fatalf("instance = %q", decoded.Record.InstanceID)
	}
	if len(decoded.Record.Messages) != 1 {
		t.Fatalf("messages = %+v", decoded.Record.Messages)
	}
	m := decoded.Record.Messages[0]
	if m.MessageID != "3EB0ABC" || m.ChatJID != "5511999999999@s.whatsapp.net" || m.SenderJID != "5511888888888@s.whatsapp.net" {
		t.Fatalf("identity = %+v", m)
	}
	if m.SenderName != "Fulano" || m.Text != "bom dia" || m.MediaType != "text" || m.FromMe || m.IsGroup {
		t.Fatalf("content = %+v", m)
	}
	if !m.SentAt.Equal(time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("sent_at = %s", m.SentAt)
	}
	if m.InstanceID != "inst-1" {
		t.Fatalf("message instance = %q", m.InstanceID)
	}
}

// Redeliveries must collapse onto the same row, so the identifier is derived
// from the message rather than from the payload bytes, which carry a
// per-delivery timestamp in some events.
func TestDecodeGivesMessagesAStableIdentity(t *testing.T) {
	raw := []byte(`{"event":"Message","instanceId":"inst-1","data":{"Info":{"ID":"3EB0ABC","Chat":"a@s.whatsapp.net","Timestamp":"2026-09-14T10:00:00Z"},"Message":{"conversation":"oi"}}}`)
	first, _ := Decode(raw)
	second, _ := Decode(raw)
	if first.Record.ID != second.Record.ID {
		t.Fatalf("ids differ: %q vs %q", first.Record.ID, second.Record.ID)
	}
	if first.Record.ID != "inst-1:Message:3EB0ABC" {
		t.Fatalf("id = %q", first.Record.ID)
	}
}

// Messages the account itself sends arrive on their own queue and are the other
// half of every conversation, so they must be indexed too.
func TestDecodeIndexesOutgoingMessages(t *testing.T) {
	raw := []byte(`{"event":"SendMessage","instanceId":"inst-1","data":{"Info":{"ID":"OUT1","Chat":"a@s.whatsapp.net","IsFromMe":true,"Timestamp":"2026-09-14T10:00:00Z"},"Message":{"extendedTextMessage":{"text":"respondendo"}}}}`)
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Record.Messages) != 1 || !decoded.Record.Messages[0].FromMe || decoded.Record.Messages[0].Text != "respondendo" {
		t.Fatalf("messages = %+v", decoded.Record.Messages)
	}
}

// A history sync delivers whole conversations in WhatsApp's own protobuf shape,
// which is why one event yields many messages.
func TestDecodeHistorySyncYieldsEveryMessage(t *testing.T) {
	raw := []byte(`{
      "event":"HistorySync",
      "instanceId":"inst-1",
      "data":{"Data":{"syncType":"ON_DEMAND","conversations":[
        {"id":"120363@g.us","messages":[
          {"message":{"key":{"id":"H1","remoteJid":"120363@g.us","fromMe":false,"participant":"5511777777777@s.whatsapp.net"},"message":{"conversation":"antiga"},"messageTimestamp":1757845200,"pushName":"Beltrano"}},
          {"message":{"key":{"id":"H2","remoteJid":"120363@g.us","fromMe":true},"message":{"imageMessage":{"caption":"foto"}},"messageTimestamp":"1757845260"}}
        ]}
      ]}}}`)
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != KindHistory {
		t.Fatalf("kind = %q", decoded.Kind)
	}
	if len(decoded.Record.Messages) != 2 {
		t.Fatalf("messages = %+v", decoded.Record.Messages)
	}
	first, second := decoded.Record.Messages[0], decoded.Record.Messages[1]
	if first.MessageID != "H1" || first.Text != "antiga" || first.SenderName != "Beltrano" || !first.IsGroup {
		t.Fatalf("first = %+v", first)
	}
	if first.SenderJID != "5511777777777@s.whatsapp.net" {
		t.Fatalf("participant lost: %+v", first)
	}
	if !first.SentAt.Equal(time.Unix(1757845200, 0).UTC()) {
		t.Fatalf("sent_at = %s", first.SentAt)
	}
	if second.MessageID != "H2" || second.Text != "foto" || second.MediaType != "image" || !second.FromMe {
		t.Fatalf("second = %+v", second)
	}
}

// Connection events are the primary signal for session state, and the reason
// they carry is the only explanation the operator gets.
func TestDecodeConnectionEvents(t *testing.T) {
	cases := []struct {
		raw    string
		state  ConnectionState
		reason string
	}{
		{`{"event":"Connected","instanceId":"i","data":{"status":"open","jid":"5511@s.whatsapp.net","pushName":"Eu"}}`, StateConnected, ""},
		{`{"event":"LoggedOut","instanceId":"i","data":{"reason":"401: logged out from another device"}}`, StateLoggedOut, "401: logged out from another device"},
		{`{"event":"Disconnected","instanceId":"i","data":{}}`, StateDisconnected, ""},
		{`{"event":"ConnectFailure","instanceId":"i","data":{"reason":"503"}}`, StateFailed, "503"},
		{`{"event":"TemporaryBan","instanceId":"i","data":{"reason":"spam"}}`, StateBanned, "spam"},
		{`{"event":"PairSuccess","instanceId":"i","data":{}}`, StatePairing, ""},
	}
	for _, c := range cases {
		decoded, err := Decode([]byte(c.raw))
		if err != nil {
			t.Fatalf("%s: %v", c.raw, err)
		}
		if decoded.Kind != KindConnection || decoded.Connection == nil {
			t.Fatalf("%s: kind=%q connection=%v", c.raw, decoded.Kind, decoded.Connection)
		}
		if decoded.Connection.State != c.state || decoded.Connection.Reason != c.reason {
			t.Fatalf("%s: got %+v", c.raw, decoded.Connection)
		}
		if len(decoded.Record.Messages) != 0 {
			t.Fatalf("%s: connection event produced messages", c.raw)
		}
	}
	connected, _ := Decode([]byte(`{"event":"Connected","instanceId":"i","data":{"jid":"5511@s.whatsapp.net","pushName":"Eu"}}`))
	if connected.Connection.JID != "5511@s.whatsapp.net" || connected.Connection.PushName != "Eu" {
		t.Fatalf("connected = %+v", connected.Connection)
	}
}

// WhatsApp hides disappearing, view-once and captioned-document content behind
// wrappers. Missing them would drop real text out of the search index.
func TestMessageTextUnwrapsNestedContent(t *testing.T) {
	cases := map[string]string{
		`{"conversation":"simples"}`:                                                                     "simples",
		`{"ephemeralMessage":{"message":{"conversation":"some em 7 dias"}}}`:                             "some em 7 dias",
		`{"viewOnceMessageV2":{"message":{"imageMessage":{"caption":"visualização única"}}}}`:            "visualização única",
		`{"documentWithCaptionMessage":{"message":{"documentMessage":{"caption":"contrato assinado"}}}}`: "contrato assinado",
		`{"documentMessage":{"fileName":"nota.pdf"}}`:                                                    "nota.pdf",
		`{"audioMessage":{"seconds":12}}`:                                                                "",
	}
	for raw, want := range cases {
		if got := messageText(json.RawMessage(raw)); got != want {
			t.Errorf("messageText(%s) = %q, want %q", raw, got, want)
		}
	}
	if got := mediaType(json.RawMessage(`{"audioMessage":{"seconds":12}}`)); got != "audio" {
		t.Errorf("audio media type = %q", got)
	}
	if got := mediaType(json.RawMessage(`{"ephemeralMessage":{"message":{"audioMessage":{}}}}`)); got != "audio" {
		t.Errorf("wrapped audio media type = %q", got)
	}
}

// The payload is remote input, so a self-referential wrapper must not drive
// unbounded recursion.
func TestMessageTextStopsAtNestingLimit(t *testing.T) {
	nested := `{"conversation":"fundo"}`
	for i := 0; i < 12; i++ {
		nested = `{"ephemeralMessage":{"message":` + nested + `}}`
	}
	if got := messageText(json.RawMessage(nested)); got != "" {
		t.Fatalf("deep nesting returned %q; the bound should stop the walk", got)
	}
}

// An unknown event still has to be stored: the raw payload is the only record
// of what happened and can be reinterpreted once the shape is understood.
func TestDecodeKeepsUnknownEvents(t *testing.T) {
	raw := []byte(`{"event":"LabelEdit","instanceId":"i","data":{"whatever":1}}`)
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != KindOther || decoded.Record.Type != "LabelEdit" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if string(decoded.Record.Payload) != string(raw) {
		t.Fatal("payload was not preserved")
	}
	if decoded.Record.ID == "" {
		t.Fatal("unknown event has no identity")
	}
}

func TestDecodeRejectsMalformedPayload(t *testing.T) {
	if _, err := Decode([]byte(`{`)); err == nil {
		t.Fatal("malformed payload decoded without error")
	}
}
