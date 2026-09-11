package events

import "testing"

func TestDecodeEvolutionMessage(t *testing.T) {
	raw := []byte(`{"event":"messages.upsert","instance":"primary","data":{"key":{"id":"msg-1","remoteJid":"1555123@s.whatsapp.net","fromMe":false},"message":{"conversation":"hello"},"messageTimestamp":1720000000}}`)
	event, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "msg-1" || event.Message == nil || event.Message.Text != "hello" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestDecodeCreatesStableIDWhenEnvelopeHasNone(t *testing.T) {
	raw := []byte(`{"event":"connection.update","data":{"state":"open"}}`)
	a, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.ID != b.ID {
		t.Fatalf("IDs are not stable: %q %q", a.ID, b.ID)
	}
}

func TestDecodeRejectsInvalidJSON(t *testing.T) {
	if _, err := Decode([]byte(`not-json`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
