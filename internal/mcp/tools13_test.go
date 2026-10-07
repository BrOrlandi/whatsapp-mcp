package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

const (
	ana   = "5511911111111@s.whatsapp.net"
	group = "120363000000000001@g.us"
)

func conversation() []store.Message {
	at := time.Now().Add(-3 * time.Hour).UTC()
	return []store.Message{
		{MessageID: "A1", ChatJID: ana, SenderJID: ana, SenderName: "Ana", Text: "oi, tudo bem?", MediaType: "text", SentAt: at},
		{MessageID: "A2", ChatJID: ana, FromMe: true, Text: "tudo!", MediaType: "text", SentAt: at.Add(time.Minute)},
		{MessageID: "A3", ChatJID: ana, SenderJID: ana, SenderName: "Ana", Text: "vamos amanhã?", MediaType: "text", SentAt: at.Add(2 * time.Minute)},
		{MessageID: "G1", ChatJID: group, IsGroup: true, SenderJID: "5511922222222@s.whatsapp.net", SenderName: "Bia", Text: "obrigado", MediaType: "text", SentAt: at.Add(3 * time.Minute)},
		{MessageID: "G2", ChatJID: group, IsGroup: true, SenderJID: "5511922222222@s.whatsapp.net", Text: "foto", MediaType: "image", MimeType: "image/jpeg", SentAt: at.Add(4 * time.Minute)},
	}
}

func TestToolSurfaceMatchesLocal(t *testing.T) {
	local := []string{"health", "whatsapp_status", "list_chats", "get_chat_messages", "search_messages", "list_contacts", "list_groups",
		"get_group", "send_text_message", "send_media_message", "download_media", "transcribe_audio", "save_transcript", "sync_history",
		"delete_message", "edit_message", "react_to_message", "check_numbers", "get_profile_picture", "send_location", "send_poll",
		"get_poll_results", "organise_chat", "forward_message", "mark_chat_read", "send_typing", "get_message_context", "message_stats",
		"export_messages", "list_unread", "list_unanswered", "list_mentions", "mark_handled", "snooze_chat", "manage_group_participants",
		"update_group", "get_group_invite_link", "leave_group", "media_stats", "purge_media"}
	have := map[string]bool{}
	for _, d := range toolDefinitions() {
		def := d.(map[string]any)
		name := def["name"].(string)
		have[name] = true
		if ToolCategory(name) == "" {
			t.Errorf("%s has no category", name)
		}
		if _, ok := def["annotations"]; !ok {
			t.Errorf("%s has no annotations", name)
		}
	}
	for _, name := range local {
		if !have[name] {
			t.Errorf("missing %s", name)
		}
	}
	total := 0
	for _, c := range Categories() {
		total += len(c.Tools)
	}
	if total != len(have) {
		t.Errorf("categories list %d tools, tools/list %d", total, len(have))
	}
}

func TestSendTextRepliesMentionsAndDrafts(t *testing.T) {
	index := &fakeIndex{messages: conversation(), chats: []store.Chat{{ChatJID: ana, Name: "Ana"}, {ChatJID: group, Name: "Família"}}}
	live := &fakeLive{}
	server := testServer(index, live, nil)

	payload, isError := call(t, server, "send_text_message", map[string]any{"to": group, "text": "@5511922222222 pode ser", "reply_to": "G1",
		"mentions": []string{"5511922222222"}, "dry_run": true})
	if isError || payload["dry_run"] != true || payload["sent"] != false || len(live.sentText) != 0 {
		t.Fatalf("draft = %#v, sent %v", payload, live.sentText)
	}
	draft := payload["draft"].(map[string]any)
	if draft["reply_to"].(map[string]any)["message_id"] != "G1" {
		t.Fatalf("draft reply = %#v", draft)
	}

	if _, isError := call(t, server, "send_text_message", map[string]any{"to": group, "text": "pode ser", "mentions": []string{"5511922222222"}}); !isError {
		t.Fatal("a mention the text does not place must be refused")
	}
	if _, isError := call(t, server, "send_text_message", map[string]any{"to": group, "text": "oi", "reply_to": "A1"}); !isError {
		t.Fatal("a reply must quote a message of the same conversation")
	}

	payload, isError = call(t, server, "send_text_message", map[string]any{"to": group, "text": "@5511922222222 pode ser", "reply_to": "G1",
		"mentions": []string{"5511922222222"}})
	if isError {
		t.Fatalf("send failed: %#v", payload)
	}
	opts := live.sendOptions[0]
	if opts.QuotedID != "G1" || opts.QuotedParticipant != "5511922222222@s.whatsapp.net" || len(opts.Mentions) != 1 || opts.Mentions[0] != "5511922222222@s.whatsapp.net" {
		t.Fatalf("options = %+v", opts)
	}

	// A name resolves to the one chat that has it.
	if _, isError := call(t, server, "send_text_message", map[string]any{"to": "Ana", "text": "oi"}); isError {
		t.Fatal("a name matching one chat must send")
	}
	if live.sentText[len(live.sentText)-1] != ana+"|oi" {
		t.Fatalf("sent = %v", live.sentText)
	}
}

func TestForwardResendsMarkedAsForwarded(t *testing.T) {
	index := &fakeIndex{messages: conversation(), raw: []byte(`{"event":"Message","data":{"Message":{"imageMessage":{"caption":"foto"}}}}`)}
	live := &fakeLive{media: &evolution.Media{Base64: "data:image/jpeg;base64,/9j/AAAA"}}
	server := testServer(index, live, nil).WithPublicURL("https://mcp.example").WithInternalURL("http://whatsapp-mcp:8080")

	payload, isError := call(t, server, "forward_message", map[string]any{"message_id": "A3", "to": group})
	if isError || !live.sendOptions[0].Forwarded || live.sentText[0] != group+"|vamos amanhã?" {
		t.Fatalf("text forward = %#v %v %+v", payload, live.sentText, live.sendOptions)
	}
	payload, isError = call(t, server, "forward_message", map[string]any{"message_id": "G2", "to": ana})
	if isError {
		t.Fatalf("media forward failed: %#v", payload)
	}
	parts := strings.Split(live.sentMedia[0], "|")
	if parts[1] != "image" || !strings.HasPrefix(parts[2], "http://whatsapp-mcp:8080/media/") || parts[3] != "foto" || !live.sendOptions[1].Forwarded {
		t.Fatalf("media forward = %v %+v", live.sentMedia, live.sendOptions)
	}
	// The file Evolution fetches is the one the gateway kept.
	token := strings.TrimPrefix(parts[2], "http://whatsapp-mcp:8080/media/")
	rec := httptest.NewRecorder()
	server.MediaHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+token, nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("own link = %d %q", rec.Code, rec.Body.String())
	}
}

func TestMarkReadTypingAndOrganiseRecordState(t *testing.T) {
	index := &fakeIndex{messages: conversation(), unread: []store.Chat{{ChatJID: ana, UnreadCount: 1}}}
	live := &fakeLive{}
	server := testServer(index, live, nil)
	if payload, isError := call(t, server, "mark_chat_read", map[string]any{"chat_jid": ana}); isError {
		t.Fatalf("mark read: %#v", payload)
	}
	if live.actions[0] != "markread|"+ana+"|A3" || len(index.reads) != 1 {
		t.Fatalf("actions = %v reads = %v", live.actions, index.reads)
	}
	if _, isError := call(t, server, "mark_chat_read", map[string]any{"chat_jid": ana, "receipts": false}); isError || len(live.actions) != 1 || len(index.reads) != 2 {
		t.Fatalf("receipts false must only mark the index: %v %v", live.actions, index.reads)
	}
	if _, isError := call(t, server, "send_typing", map[string]any{"to": ana, "audio": true}); isError || live.actions[1] != "presence|"+ana+"|composing+audio" {
		t.Fatalf("typing = %v", live.actions)
	}
	if _, isError := call(t, server, "organise_chat", map[string]any{"chat_jid": ana, "action": "archive"}); isError || index.flags[0] != ana+"|archive" {
		t.Fatalf("flags = %v", index.flags)
	}
}

func TestReadsShapeAndContext(t *testing.T) {
	index := &fakeIndex{messages: conversation(), stats: []store.Bucket{{Key: ana, Count: 3}}, names: map[string]string{ana: "Ana"}}
	server := testServer(index, &fakeLive{}, nil)

	payload, isError := call(t, server, "get_chat_messages", map[string]any{"chat_jid": ana, "fields": []string{"message_id", "text"}, "max_content_chars": 3})
	if isError {
		t.Fatalf("read: %#v", payload)
	}
	first := payload["messages"].([]any)[0].(map[string]any)
	if len(first) != 3 || first["text"] != "oi,…" || first["text_truncated"] != true {
		t.Fatalf("shaped = %#v", first)
	}
	if _, isError := call(t, server, "get_chat_messages", map[string]any{"chat_jid": ana, "fields": []string{"nope"}}); !isError {
		t.Fatal("an unknown field must be refused")
	}
	payload, _ = call(t, server, "get_chat_messages", map[string]any{"chat_jid": ana, "count_only": true})
	if payload["count"] != float64(5) || payload["chat_name"] != "Ana" {
		t.Fatalf("count = %#v", payload)
	}

	payload, isError = call(t, server, "get_message_context", map[string]any{"message_id": "A2", "before": 1, "after": 1})
	if isError || len(payload["before"].([]any)) != 1 || len(payload["after"].([]any)) != 1 {
		t.Fatalf("context = %#v", payload)
	}

	payload, isError = call(t, server, "message_stats", map[string]any{"direction": "in", "since": "2026-09-01"})
	if isError || index.lastFilter.Direction != "in" || index.lastFilter.Since.IsZero() {
		t.Fatalf("stats = %#v filter %+v", payload, index.lastFilter)
	}
	if buckets := payload["buckets"].([]any); buckets[0].(map[string]any)["name"] != "Ana" {
		t.Fatalf("buckets = %#v", buckets)
	}
}

func TestExportHandsOverALink(t *testing.T) {
	index := &fakeIndex{messages: conversation()}
	server := testServer(index, &fakeLive{}, nil).WithPublicURL("https://mcp.example")
	payload, isError := call(t, server, "export_messages", map[string]any{"chat_jid": ana})
	if isError || payload["count"] != float64(3) || !strings.HasPrefix(payload["url"].(string), "https://mcp.example/media/") {
		t.Fatalf("export = %#v", payload)
	}
	token := strings.TrimPrefix(payload["url"].(string), "https://mcp.example/media/")
	rec := httptest.NewRecorder()
	server.MediaHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+token, nil))
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	var m store.Message
	if rec.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &m) != nil || m.MessageID != "A1" {
		t.Fatalf("export body = %d %q", rec.Code, rec.Body.String())
	}
	inv, err := server.Inventory(context.Background(), "")
	if err != nil || inv.Exports.Files != 1 {
		t.Fatalf("inventory = %+v, %v", inv, err)
	}
}

func TestTriageLists(t *testing.T) {
	msgs := conversation()
	index := &fakeIndex{messages: msgs, own: []string{"5511900000000@s.whatsapp.net"}, mentions: []store.Message{msgs[3]},
		unread: []store.Chat{{ChatJID: ana, Name: "Ana", UnreadCount: 1}}}
	server := testServer(index, &fakeLive{}, nil)

	payload, isError := call(t, server, "list_unread", nil)
	if isError || payload["count"] != float64(1) {
		t.Fatalf("unread = %#v", payload)
	}
	chat := payload["chats"].([]any)[0].(map[string]any)
	if len(chat["messages"].([]any)) != 1 {
		t.Fatalf("unread messages = %#v", chat)
	}

	payload, _ = call(t, server, "list_unanswered", nil)
	if payload["count"] != float64(1) || payload["chats"].([]any)[0].(map[string]any)["chat_jid"] != ana {
		t.Fatalf("unanswered = %#v", payload)
	}
	// The group's last word is from someone else, but groups are left out
	// unless asked; asked, its "obrigado"... is followed by a photo, so it waits.
	payload, _ = call(t, server, "list_unanswered", map[string]any{"include_groups": true})
	if payload["count"] != float64(2) {
		t.Fatalf("with groups = %#v", payload)
	}

	if _, isError := call(t, server, "mark_handled", map[string]any{"chat_jid": ana, "note": "respondi por telefone"}); isError {
		t.Fatal("mark_handled failed")
	}
	payload, _ = call(t, server, "list_unanswered", nil)
	if payload["count"] != float64(0) {
		t.Fatalf("a handled chat must leave the list: %#v", payload)
	}
	payload, _ = call(t, server, "list_unanswered", map[string]any{"include_handled": true})
	if payload["count"] != float64(1) || payload["chats"].([]any)[0].(map[string]any)["handled"] != true {
		t.Fatalf("include_handled = %#v", payload)
	}
	if _, isError := call(t, server, "snooze_chat", map[string]any{"chat_jid": ana, "until": "2000-01-01"}); !isError {
		t.Fatal("a snooze into the past must be refused")
	}
	if _, isError := call(t, server, "mark_handled", map[string]any{"chat_jid": ana, "clear": true}); isError || len(index.marks) != 0 {
		t.Fatal("clear must forget the mark")
	}

	payload, isError = call(t, server, "list_mentions", nil)
	if isError || payload["count"] != float64(1) {
		t.Fatalf("mentions = %#v", payload)
	}
}

func TestClosingsDoNotWait(t *testing.T) {
	for text, closing := range map[string]bool{"Obrigado!": true, "ok": true, "👍": true, "@5511900000000 valeu": true, "e amanhã?": false} {
		m := store.Message{Text: text, MediaType: "text"}
		if got := isClosing(m, []string{"5511900000000@s.whatsapp.net"}); got != closing {
			t.Errorf("isClosing(%q) = %v", text, got)
		}
	}
	if !isClosing(store.Message{MediaType: "sticker"}, nil) {
		t.Error("a sticker closes")
	}
}

func TestGroupToolsAskBeforeTheIrreversible(t *testing.T) {
	live := &fakeLive{group: evolution.Group{JID: group, Name: "Família", MemberCount: 5}, inviteLink: "ABC"}
	server := testServer(&fakeIndex{}, live, nil)

	payload, _ := call(t, server, "manage_group_participants", map[string]any{"group_jid": group, "action": "remove", "participants": []string{"5511922222222"}})
	if payload["preview"] != true || len(live.actions) != 0 {
		t.Fatalf("remove without confirm = %#v %v", payload, live.actions)
	}
	call(t, server, "manage_group_participants", map[string]any{"group_jid": group, "action": "remove", "participants": []string{"5511922222222"}, "confirm": true})
	if live.actions[0] != "participants|"+group+"|remove|5511922222222@s.whatsapp.net" {
		t.Fatalf("actions = %v", live.actions)
	}
	call(t, server, "manage_group_participants", map[string]any{"group_jid": group, "action": "promote", "participants": []string{"5511922222222@s.whatsapp.net"}})

	payload, _ = call(t, server, "get_group_invite_link", map[string]any{"group_jid": group})
	if payload["link"] != "https://chat.whatsapp.com/ABC" {
		t.Fatalf("link = %#v", payload)
	}
	payload, _ = call(t, server, "get_group_invite_link", map[string]any{"group_jid": group, "reset": true})
	if payload["preview"] != true {
		t.Fatalf("reset without confirm = %#v", payload)
	}
	payload, _ = call(t, server, "leave_group", map[string]any{"group_jid": group})
	if payload["preview"] != true || payload["participants"] != float64(5) {
		t.Fatalf("leave without confirm = %#v", payload)
	}
	call(t, server, "update_group", map[string]any{"group_jid": group, "name": "Família Silva", "description": ""})
	if !containsAll(live.actions, "name|"+group+"|Família Silva", "description|"+group+"|") {
		t.Fatalf("update = %v", live.actions)
	}
	if _, isError := call(t, server, "leave_group", map[string]any{"group_jid": "5511@s.whatsapp.net"}); !isError {
		t.Fatal("a non-group JID must be refused")
	}
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestMediaIsKeptAndPurged(t *testing.T) {
	index := &fakeIndex{messages: conversation(), raw: []byte(`{"event":"Message","data":{"Message":{"imageMessage":{}}}}`)}
	live := &fakeLive{media: &evolution.Media{Base64: "data:image/jpeg;base64,/9j/AAAA"}}
	server := testServer(index, live, nil)

	result := callRaw(t, server, "download_media", map[string]any{"message_id": "G2"})
	content := result["content"].([]any)
	if len(content) != 2 || content[1].(map[string]any)["type"] != "image" {
		t.Fatalf("content = %#v", content)
	}
	// A second download is served from the kept copy, without asking WhatsApp.
	live.mediaErr = evolution.ErrMediaExpired
	result = callRaw(t, server, "download_media", map[string]any{"message_id": "G2"})
	if result["isError"] == true {
		t.Fatalf("kept copy not used: %#v", result)
	}

	payload, _ := call(t, server, "media_stats", nil)
	inv := payload["inventory"].(map[string]any)
	if inv["files"] != float64(1) {
		t.Fatalf("stats = %#v", payload)
	}
	payload, _ = call(t, server, "purge_media", nil)
	if payload["preview"] != true || payload["would_delete_files"] != float64(1) {
		t.Fatalf("purge preview = %#v", payload)
	}
	payload, _ = call(t, server, "purge_media", map[string]any{"confirm": true})
	if payload["deleted_files"] != float64(1) {
		t.Fatalf("purge = %#v", payload)
	}
	if files, _ := filepath.Glob(filepath.Join(server.mediaDir, "*", "*", "*")); len(files) != 0 {
		t.Fatalf("left behind: %v", files)
	}
	if err := server.SetRetention(context.Background(), 30); err != nil || server.retentionDays(context.Background()) != 30 {
		t.Fatal("retention not kept")
	}
	_ = os.RemoveAll(server.mediaDir)
}

func TestDeleteForMeIsExplained(t *testing.T) {
	server := testServer(&fakeIndex{messages: conversation()}, &fakeLive{}, nil)
	payload, isError := call(t, server, "delete_message", map[string]any{"message_id": "A1", "for_me": true})
	if !isError || !strings.Contains(payload["error"].(string), "not available on the server version") {
		t.Fatalf("for_me = %#v", payload)
	}
}

func TestHealthGivesOneVerdict(t *testing.T) {
	server := testServer(&fakeIndex{messages: conversation()}, &fakeLive{}, nil)
	payload, isError := call(t, server, "health", nil)
	if isError || payload["status"] != "ok" {
		t.Fatalf("health = %#v", payload)
	}
	payload, _ = call(t, server, "health", map[string]any{"max_silence_hours": 1})
	if payload["status"] != "warn" {
		t.Fatalf("a quiet account past max_silence_hours warns: %#v", payload)
	}
}

func TestInitializeMentionsWebhooks(t *testing.T) {
	server := testServer(&fakeIndex{}, &fakeLive{}, nil).WithPublicURL("https://mcp.example")
	var response map[string]any
	_ = json.Unmarshal(server.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)), &response)
	text := response["result"].(map[string]any)["instructions"].(string)
	if !strings.Contains(text, "https://mcp.example/configuracoes#webhooks") || !strings.Contains(text, "third parties") {
		t.Fatalf("instructions = %q", text)
	}
}

// callRaw runs one tool and returns its whole result, content blocks
// included.
func callRaw(t *testing.T, s *Server, name string, args map[string]any) map[string]any {
	t.Helper()
	encoded, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	var response map[string]any
	if err := json.Unmarshal(s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(encoded)+`}`)), &response); err != nil {
		t.Fatal(err)
	}
	return response["result"].(map[string]any)
}
