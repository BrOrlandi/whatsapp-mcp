package webhook

import (
	"context"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
)

type names map[string]string

func (n names) ChatName(_ context.Context, _ string, jid string) string { return n[jid] }

func decode(t *testing.T, raw string) events.Event {
	t.Helper()
	decoded, err := events.Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestFromEventShapesWhatLocalSends(t *testing.T) {
	n := names{"120363000000000001@g.us": "Família"}
	photo := decode(t, `{"event":"Message","instanceId":"i","data":{"Info":{"ID":"X1","Chat":"120363000000000001@g.us","Sender":"5511900000001@s.whatsapp.net","IsGroup":true,"PushName":"Mãe","Timestamp":"2026-10-07T10:00:00Z"},
		"Message":{"imageMessage":{"caption":"olha","mimetype":"image/jpeg","fileLength":1234,"contextInfo":{"stanzaID":"Q0","participant":"5511900000002@s.whatsapp.net","quotedMessage":{"conversation":"cadê a foto?"}}}}}}`)
	evs := FromEvent(context.Background(), photo, n)
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	m := evs[0].Message
	if evs[0].Kind != "message" || evs[0].InstanceID != "i" || !m.Group || m.ChatName != "Família" || m.SenderName != "Mãe" || m.Text != "olha" ||
		m.Media == nil || m.Media.Bytes != 1234 || m.ReplyTo == nil || m.ReplyTo.Text != "cadê a foto?" {
		t.Fatalf("message = %+v %+v", evs[0], m)
	}

	reaction := decode(t, `{"event":"Message","instanceId":"i","data":{"Info":{"ID":"X2","Chat":"5511900000001@s.whatsapp.net","Sender":"5511900000001@s.whatsapp.net","Timestamp":"2026-10-07T10:01:00Z"},"Message":{"reactionMessage":{"key":{"ID":"X1"},"text":"❤️"}}}}`)
	if evs := FromEvent(context.Background(), reaction, n); len(evs) != 1 || evs[0].Kind != "reaction" || evs[0].Message.Reaction.Emoji != "❤️" {
		t.Fatalf("reaction = %+v", evs)
	}

	edit := decode(t, `{"event":"SendMessage","instanceId":"i","data":{"Info":{"ID":"P1","Chat":"5511900000001@s.whatsapp.net","IsFromMe":true,"Timestamp":"2026-10-07T10:02:00Z"},"Message":{"protocolMessage":{"key":{"ID":"X0"},"type":14,"editedMessage":{"conversation":"corrigido"}}}}}`)
	evs = FromEvent(context.Background(), edit, n)
	if len(evs) != 1 || evs[0].Message.ID != "X0" || !evs[0].Message.Edited || evs[0].Message.Text != "corrigido" || !evs[0].fromMe {
		t.Fatalf("edit = %+v", evs)
	}

	receipt := decode(t, `{"event":"Receipt","instanceId":"i","data":{"Chat":"5511900000001@s.whatsapp.net","Sender":"5511900000001@s.whatsapp.net","MessageIDs":["Y"],"Timestamp":"2026-10-07T10:03:00Z","Type":""}}`)
	if evs := FromEvent(context.Background(), receipt, n); len(evs) != 1 || evs[0].Kind != "receipt" || evs[0].Receipt.Type != "delivered" {
		t.Fatalf("receipt = %+v", evs)
	}

	vote := decode(t, `{"event":"Message","instanceId":"i","data":{"Info":{"ID":"V1","Chat":"5511900000001@s.whatsapp.net","Timestamp":"2026-10-07T10:04:00Z"},"Message":{"pollUpdateMessage":{}}}}`)
	if evs := FromEvent(context.Background(), vote, n); len(evs) != 0 {
		t.Fatalf("a poll vote carries nothing to read: %+v", evs)
	}
	history := decode(t, `{"event":"HistorySync","instanceId":"i","data":{"Data":{"conversations":[]}}}`)
	if evs := FromEvent(context.Background(), history, n); len(evs) != 0 {
		t.Fatal("history is not delivered")
	}
}
