package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// These tests run the SQL against a real PostgreSQL. They are skipped unless
// TEST_DATABASE_URL points at a database they may wipe, for example:
//
//	docker run -d --rm -e POSTGRES_PASSWORD=test -p 127.0.0.1:55432:5432 postgres:17.6
//	TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/store/
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("wamcp_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
		admin.Close()
	})
	testDSN := strings.Replace(dsn, "/postgres?", "/"+name+"?", 1)
	s, err := store.Open(ctx, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Count unread from the start of time, so the fixtures below are counted.
	if err := s.SetSetting(ctx, "unread_tracking_since", "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return s
}

func persist(t *testing.T, s *store.Store, raw string) events.Event {
	t.Helper()
	decoded, err := events.Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PersistEvent(context.Background(), decoded.Record); err != nil {
		t.Fatalf("persist %s: %v", raw, err)
	}
	return decoded
}

func live(event, id, chat, sender string, fromMe bool, at string, message string) string {
	group := strings.HasSuffix(chat, "@g.us")
	return fmt.Sprintf(`{"event":%q,"instanceId":"i","data":{"Info":{"ID":%q,"Chat":%q,"Sender":%q,"IsFromMe":%v,"IsGroup":%v,"PushName":"Fulano","Timestamp":%q},"Message":%s}}`,
		event, id, chat, sender, fromMe, group, at, message)
}

func TestReadsAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	const (
		ana   = "5511911111111@s.whatsapp.net"
		me    = "5511900000000@s.whatsapp.net"
		group = "120363000000000001@g.us"
		myLID = "99999@lid"
	)
	persist(t, s, live("Message", "A1", ana, ana, false, "2026-10-01T10:00:00Z", `{"conversation":"oi, tudo bem?"}`))
	persist(t, s, live("SendMessage", "A2", ana, me, true, "2026-10-01T10:05:00Z", `{"extendedTextMessage":{"text":"tudo!","contextInfo":{"stanzaID":"A1","participant":"`+ana+`"}}}`))
	persist(t, s, live("Message", "A3", ana, ana, false, "2026-10-01T11:00:00Z", `{"conversation":"vamos amanhã?"}`))
	persist(t, s, live("Message", "A4", ana, ana, false, "2026-10-01T11:01:00Z", `{"reactionMessage":{"key":{"ID":"A2"},"text":"❤️"}}`))
	persist(t, s, live("Message", "G1", group, ana, false, "2026-10-01T12:00:00Z", `{"extendedTextMessage":{"text":"@99999 você vem?","contextInfo":{"mentionedJID":["`+myLID+`"]}}}`))
	persist(t, s, live("SendMessage", "G0", group, myLID, true, "2026-10-01T09:00:00Z", `{"conversation":"bom dia"}`))
	persist(t, s, live("Message", "G2", group, ana, false, "2026-10-01T12:30:00Z", `{"imageMessage":{"caption":"olha","mimetype":"image/jpeg","fileLength":"1234","contextInfo":{"isForwarded":true,"forwardingScore":2}}}`))
	// An edit and a deletion change the messages they point at.
	persist(t, s, live("Message", "E1", ana, ana, false, "2026-10-01T11:02:00Z", `{"protocolMessage":{"key":{"ID":"A3"},"type":14,"editedMessage":{"conversation":"vamos depois de amanhã?"}}}`))
	persist(t, s, live("Message", "R1", group, ana, false, "2026-10-01T12:31:00Z", `{"protocolMessage":{"key":{"ID":"G2"},"type":0}}`))

	got, err := s.Messages(ctx, "i", store.MessageQuery{ChatJID: ana, Oldest: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("chat messages = %d (%+v), want 4: the edit and its target collapse, the reaction stays", len(got), got)
	}
	if got[1].QuotedID != "A1" || got[2].Text != "vamos depois de amanhã?" || !got[2].Edited || got[3].ReactionTo != "A2" || got[3].Reaction != "❤️" {
		t.Fatalf("details = %+v", got)
	}

	chats, err := s.ListChats(ctx, "i", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 {
		t.Fatalf("chats = %+v", chats)
	}
	byJID := map[string]store.Chat{}
	for _, c := range chats {
		byJID[c.ChatJID] = c
	}
	if byJID[ana].UnreadCount != 1 || byJID[ana].Messages != 3 {
		t.Fatalf("ana: %+v (unread after the account's last message: A3 only; the reaction is not a message)", byJID[ana])
	}
	if byJID[group].UnreadCount != 1 {
		t.Fatalf("group: %+v (G1 unread, G2 deleted)", byJID[group])
	}

	// A read receipt from the phone clears the chat.
	persist(t, s, `{"event":"Receipt","instanceId":"i","data":{"Chat":"`+ana+`","Sender":"`+me+`","IsFromMe":true,"MessageIDs":["A3"],"Timestamp":"2026-10-01T11:30:00Z","Type":"read-self"}}`)
	unread, err := s.UnreadChats(ctx, "i", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 || unread[0].ChatJID != group {
		t.Fatalf("unread = %+v, want only the group", unread)
	}

	mentions, err := s.Mentions(ctx, "i", s.OwnIDs(ctx, "i"), "", time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(mentions) != 1 || mentions[0].MessageID != "G1" {
		t.Fatalf("mentions = %+v (own ids %v)", mentions, s.OwnIDs(ctx, "i"))
	}

	n, err := s.CountMessages(ctx, "i", store.Filter{Direction: "in"})
	if err != nil || n != 3 {
		t.Fatalf("incoming = %d, %v; want 3 (A1, A3, G1)", n, err)
	}
	buckets, total, groups, err := s.Stats(ctx, "i", store.Filter{}, "chat", "America/Sao_Paulo", 10)
	if err != nil || total != 5 || groups != 2 || buckets[0].Key != ana {
		t.Fatalf("stats = %+v total %d groups %d err %v", buckets, total, groups, err)
	}
	if _, _, _, err := s.Stats(ctx, "i", store.Filter{}, "day", "America/Sao_Paulo", 10); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Stats(ctx, "i", store.Filter{}, "sender", "UTC", 10); err != nil {
		t.Fatal(err)
	}

	target, err := s.MessageInChat(ctx, "i", "A2", ana)
	if err != nil {
		t.Fatal(err)
	}
	before, after, err := s.MessageContext(ctx, "i", target, 5, 5)
	if err != nil || len(before) != 1 || before[0].MessageID != "A1" || len(after) != 1 || after[0].MessageID != "A3" {
		t.Fatalf("context = %+v / %+v, %v", before, after, err)
	}

	last, err := s.LastMessages(ctx, "i", time.Time{})
	if err != nil || len(last) != 2 || last[0].MessageID != "G1" {
		t.Fatalf("last = %+v, %v", last, err)
	}
	waiting, since, err := s.Waiting(ctx, "i", ana)
	if err != nil || waiting != 1 || !since.Equal(time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("waiting = %d since %s, %v", waiting, since, err)
	}

	var exported []string
	if err := s.EachMessage(ctx, "i", store.Filter{ChatJID: ana}, func(m store.Message) error {
		exported = append(exported, m.MessageID)
		return nil
	}); err != nil || strings.Join(exported, ",") != "A1,A2,A3" {
		t.Fatalf("export = %v, %v", exported, err)
	}

	if err := s.MarkHandled(ctx, "i", ana, "respondido", time.Now()); err != nil {
		t.Fatal(err)
	}
	marks, err := s.Marks(ctx, "i")
	if err != nil || marks[ana].Note != "respondido" {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	if err := s.ClearMark(ctx, "i", ana); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatFlag(ctx, "i", group, "archive", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if c, ok := s.ChatInfo(ctx, "i", group); !ok || !c.Archived {
		t.Fatalf("archived group = %+v %v", c, ok)
	}
	if unread, _ := s.UnreadChats(ctx, "i", false, 10); len(unread) != 0 {
		t.Fatalf("an archived chat is left out unless asked: %+v", unread)
	}
	activity, err := s.Activity(ctx, "i", time.Date(2026, 10, 1, 12, 40, 0, 0, time.UTC))
	if err != nil || activity.LastHour != 1 || activity.NewestIncoming == nil {
		t.Fatalf("activity = %+v, %v", activity, err)
	}
}

func TestHistorySyncUnreadAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	persist(t, s, `{"event":"HistorySync","instanceId":"i","data":{"Data":{"syncType":"RECENT","conversations":[
		{"id":"5511922222222@s.whatsapp.net","unreadCount":2,"archived":true,"conversationTimestamp":1759320000,"name":"Bia","messages":[
			{"message":{"key":{"id":"H1","remoteJid":"5511922222222@s.whatsapp.net","fromMe":false},"message":{"conversation":"um"},"messageTimestamp":1759310000}},
			{"message":{"key":{"id":"H2","remoteJid":"5511922222222@s.whatsapp.net","fromMe":false},"message":{"conversation":"dois"},"messageTimestamp":1759315000}},
			{"message":{"key":{"id":"H3","remoteJid":"5511922222222@s.whatsapp.net","fromMe":false},"message":{"conversation":"três"},"messageTimestamp":1759319000}}
		]},
		{"id":"5511933333333@s.whatsapp.net","unreadCount":0,"conversationTimestamp":1759320000,"messages":[
			{"message":{"key":{"id":"H4","remoteJid":"5511933333333@s.whatsapp.net","fromMe":false},"message":{"conversation":"lido"},"messageTimestamp":1759310000}}
		]}]}}}`)
	chats, err := s.UnreadChats(ctx, "i", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].UnreadCount != 2 || !chats[0].Archived || chats[0].Name != "Bia" {
		t.Fatalf("unread = %+v", chats)
	}
}

func TestWebhooksAndSettingsAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	h := store.Webhook{ID: "abc", URL: "https://x.example/hook", Secret: "s", Events: []string{"message", "receipt"}, Enabled: true, CreatedAt: time.Now()}
	if err := s.SaveWebhook(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDelivery(ctx, "abc", time.Now(), "200 OK", true); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableWebhook(ctx, "abc", time.Now(), "parou"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Webhook(ctx, "abc")
	if err != nil || got.Delivered != 1 || got.Enabled || got.DisabledReason != "parou" || len(got.Events) != 2 || got.LastSuccessAt == nil {
		t.Fatalf("webhook = %+v, %v", got, err)
	}
	if err := s.EnableWebhook(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWebhook(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Webhook(ctx, "abc"); err != store.ErrNoWebhook {
		t.Fatalf("deleted webhook: %v", err)
	}
	if err := s.SetSetting(ctx, "media_retention_days", "30"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Setting(ctx, "media_retention_days"); v != "30" {
		t.Fatalf("setting = %q", v)
	}
}

func TestEnrichAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	quote := live("Message", "Q1", "5511944444444@s.whatsapp.net", "5511944444444@s.whatsapp.net", false, "2026-10-01T10:00:00Z",
		`{"extendedTextMessage":{"text":"isso","contextInfo":{"stanzaID":"X0"}}}`)
	edit := live("Message", "P1", "5511944444444@s.whatsapp.net", "5511944444444@s.whatsapp.net", false, "2026-10-01T10:01:00Z",
		`{"protocolMessage":{"key":{"ID":"Q1"},"type":14,"editedMessage":{"conversation":"isso aí"}}}`)
	// Rows as an older decoder wrote them: no details, the edit as an empty message.
	for _, row := range []struct{ id, raw, text string }{{"Q1", quote, "isso"}, {"P1", edit, ""}} {
		eventID := "i:Message:" + row.id
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO events (event_id,event_type,instance_id,payload,received_at) VALUES ($1,'Message','i',$2,now())`, eventID, row.raw); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO messages (instance_id,message_id,chat_jid,sender_jid,text,sent_at,event_id) VALUES ('i',$1,'5511944444444@s.whatsapp.net','5511944444444@s.whatsapp.net',$2,now(),$3)`, row.id, row.text, eventID); err != nil {
			t.Fatal(err)
		}
	}
	pending, _, err := s.DetailsPending(ctx, store.DetailsCursor{At: time.Unix(0, 0)}, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %d, %v", len(pending), err)
	}
	for _, p := range pending {
		decoded, _ := events.Decode(p.Payload)
		if err := s.Enrich(ctx, p.ID, decoded.Record); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Messages(ctx, "i", store.MessageQuery{ChatJID: "5511944444444@s.whatsapp.net"})
	if err != nil || len(got) != 1 || got[0].QuotedID != "X0" || got[0].Text != "isso aí" || !got[0].Edited {
		t.Fatalf("enriched = %+v, %v", got, err)
	}
	if pending, _, _ := s.DetailsPending(ctx, store.DetailsCursor{At: time.Unix(0, 0)}, 10); len(pending) != 0 {
		t.Fatalf("still pending: %d", len(pending))
	}
}

func TestEditsComeFromTheAuthorAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	const (
		group = "120363000000000009@g.us"
		owner = "5511955555555@s.whatsapp.net"
		other = "5511966666666@s.whatsapp.net"
	)
	persist(t, s, live("SendMessage", "M1", group, "5511900000000@s.whatsapp.net", true, "2026-10-01T10:00:00Z", `{"conversation":"original"}`))
	persist(t, s, live("Message", "M2", group, owner, false, "2026-10-01T10:01:00Z", `{"conversation":"da dona"}`))
	// Someone else claims to edit both messages: ignored.
	persist(t, s, live("Message", "E1", group, other, false, "2026-10-01T10:02:00Z", `{"protocolMessage":{"key":{"ID":"M1"},"type":14,"editedMessage":{"conversation":"forjado"}}}`))
	persist(t, s, live("Message", "E2", group, other, false, "2026-10-01T10:03:00Z", `{"protocolMessage":{"key":{"ID":"M2"},"type":14,"editedMessage":{"conversation":"forjado"}}}`))
	// The author edits; a device suffix on the sender is the same person.
	persist(t, s, live("Message", "E3", group, "5511955555555:7@s.whatsapp.net", false, "2026-10-01T10:04:00Z", `{"protocolMessage":{"key":{"ID":"M2"},"type":14,"editedMessage":{"conversation":"corrigido"}}}`))
	// In a group an admin may delete someone else's message for everyone.
	persist(t, s, live("Message", "R1", group, other, false, "2026-10-01T10:05:00Z", `{"protocolMessage":{"key":{"ID":"M1"},"type":0}}`))
	m1, _ := s.MessageByID(ctx, "i", "M1")
	m2, _ := s.MessageByID(ctx, "i", "M2")
	if m1.Text != "original" || m1.Edited || !m1.Revoked {
		t.Fatalf("M1 = %+v", m1)
	}
	if m2.Text != "corrigido" || !m2.Edited {
		t.Fatalf("M2 = %+v", m2)
	}
}

func TestChatStateEdgesAgainstPostgres(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	const bia = "5511922222222@s.whatsapp.net"
	// The phone says 5 unread; the index holds only 2 of them: both count.
	persist(t, s, `{"event":"HistorySync","instanceId":"i","data":{"Data":{"conversations":[
		{"id":"`+bia+`","unreadCount":5,"archived":true,"conversationTimestamp":1759320000,"messages":[
			{"message":{"key":{"id":"B1","remoteJid":"`+bia+`","fromMe":false},"message":{"conversation":"um"},"messageTimestamp":1759310000}},
			{"message":{"key":{"id":"B2","remoteJid":"`+bia+`","fromMe":false},"message":{"conversation":"dois"},"messageTimestamp":1759315000}}
		]}]}}}`)
	chat, ok := s.ChatInfo(ctx, "i", bia)
	if !ok || chat.UnreadCount != 2 || !chat.Archived {
		t.Fatalf("chat = %+v", chat)
	}
	// A later chunk that leaves the flags out keeps them.
	persist(t, s, `{"event":"HistorySync","instanceId":"i","data":{"Data":{"syncType":"ON_DEMAND","conversations":[{"id":"`+bia+`","messages":[]}]}}}`)
	if chat, _ = s.ChatInfo(ctx, "i", bia); !chat.Archived {
		t.Fatalf("a chunk without flags reset them: %+v", chat)
	}
	// A new message takes the chat out of the archive, as WhatsApp does.
	persist(t, s, live("Message", "B3", bia, bia, false, "2026-10-02T10:00:00Z", `{"conversation":"oi de novo"}`))
	if chat, _ = s.ChatInfo(ctx, "i", bia); chat.Archived || chat.UnreadCount != 3 {
		t.Fatalf("after a new message = %+v", chat)
	}
}
