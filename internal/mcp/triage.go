package mcp

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// preview is one message as the triage lists show it: enough to decide, not
// the whole conversation.
type preview struct {
	ID        string    `json:"message_id"`
	SentAt    time.Time `json:"sent_at"`
	Sender    string    `json:"sender,omitempty"`
	SenderJID string    `json:"sender_jid,omitempty"`
	Text      string    `json:"text,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
}

func (n *namer) preview(ctx context.Context, m store.Message) preview {
	sender := m.SenderName
	if sender == "" && !m.FromMe {
		sender = n.name(ctx, m.SenderJID)
	}
	p := preview{ID: m.MessageID, SentAt: m.SentAt, Sender: sender, SenderJID: m.SenderJID, Text: clip(m.Text, 300), MediaType: m.MediaType}
	if p.MediaType == "text" {
		p.MediaType = ""
	}
	if m.MediaType == "audio" && p.Text == "" && m.Transcript != "" {
		p.Text = clip("[transcript] "+m.Transcript, 300)
	}
	return p
}

func (s *Server) marks(ctx context.Context, session Session) map[string]store.Mark {
	marks, err := s.index.Marks(ctx, session.InstanceID)
	if err != nil {
		return map[string]store.Mark{}
	}
	return marks
}

func defaultLimit(v, def, most int) int {
	if v <= 0 {
		return def
	}
	return min(v, most)
}

func (s *Server) listUnread(ctx context.Context, session Session, a arguments) map[string]any {
	chats, err := s.index.UnreadChats(ctx, session.InstanceID, a.IncludeArchived, defaultLimit(a.Limit, 20, 100))
	if err != nil {
		return toolError("could not read the conversations: %v", err)
	}
	perChat := defaultLimit(a.PerChat, 5, 20)
	includeMuted := a.IncludeMuted == nil || *a.IncludeMuted
	marks := s.marks(ctx, session)
	names := s.namer(session)
	now := time.Now()
	out := []map[string]any{}
	var total int64
	for _, c := range chats {
		if c.Muted(now) && !includeMuted {
			continue
		}
		n := 1
		if c.UnreadCount > 0 {
			n = min(int(c.UnreadCount), perChat)
		}
		rows, err := s.index.Incoming(ctx, session.InstanceID, c.ChatJID, n)
		if err != nil {
			return toolError("could not read the messages: %v", err)
		}
		msgs := make([]preview, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			msgs = append(msgs, names.preview(ctx, rows[i]))
		}
		name := c.Name
		if name == "" {
			name = names.name(ctx, c.ChatJID)
		}
		row := map[string]any{"chat_jid": c.ChatJID, "name": name, "group": c.IsGroup, "unread": c.UnreadCount,
			"last_message_at": c.LastMessageAt, "messages": msgs}
		if c.MarkedUnread {
			row["marked_unread"] = true
		}
		if c.Muted(now) {
			row["muted"] = true
		}
		if c.Archived {
			row["archived"] = true
		}
		if m, ok := marks[c.ChatJID]; ok && len(rows) > 0 && m.Hides(rows[0].SentAt, now) {
			row["handled"] = true
		}
		total += c.UnreadCount
		out = append(out, row)
	}
	return textResult(map[string]any{"chats": out, "count": len(out), "unread_messages": total,
		"note":            "unread as the phone shows it: reading a chat on the phone clears it here too, as WhatsApp reports the read. messages are the latest received in each chat, up to per_chat",
		"content_warning": UntrustedContent}, false)
}

// closings are the short replies that end an exchange rather than ask for
// an answer: a chat whose last message is one of them is not waiting.
var closings = map[string]bool{
	"ok": true, "okay": true, "okk": true, "blz": true, "beleza": true, "obrigado": true, "obrigada": true, "obg": true,
	"brigado": true, "brigada": true, "valeu": true, "vlw": true, "tmj": true, "show": true, "top": true, "perfeito": true,
	"combinado": true, "fechado": true, "certo": true, "certinho": true, "de nada": true, "imagina": true, "otimo": true,
	"ótimo": true, "massa": true, "joia": true, "jóia": true, "thanks": true, "thank you": true, "thx": true, "ty": true,
	"👍": true, "🙏": true, "❤️": true, "❤": true, "👌": true, "😘": true, "🙌": true, "👏": true,
}

// isClosing reports whether a message only closes the exchange: a sticker,
// or a short thanks or ok, mentions of the account aside.
func isClosing(m store.Message, ownIDs []string) bool {
	if m.MediaType == "sticker" {
		return true
	}
	if m.MediaType != "" && m.MediaType != "text" {
		return false
	}
	t := strings.ToLower(m.Text)
	for _, id := range ownIDs {
		if user, _, _ := strings.Cut(id, "@"); user != "" {
			t = strings.ReplaceAll(t, "@"+user, "")
		}
	}
	t = strings.TrimFunc(t, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(".,!?;:~", r) })
	t = strings.Join(strings.Fields(t), " ")
	return closings[t]
}

func (s *Server) listUnanswered(ctx context.Context, session Session, a arguments) map[string]any {
	since := time.Now().AddDate(0, 0, -30)
	if a.Since != "" {
		t, err := parseWhen(a.Since, "since")
		if err != nil {
			return toolError("%v", err)
		}
		since = t
	}
	last, err := s.index.LastMessages(ctx, session.InstanceID, since)
	if err != nil {
		return toolError("could not read the conversations: %v", err)
	}
	ignoreClosing := a.IgnoreClosing == nil || *a.IgnoreClosing
	includeMuted := a.IncludeMuted == nil || *a.IncludeMuted
	own := s.ownIDs(ctx, session)
	marks := s.marks(ctx, session)
	names := s.namer(session)
	now := time.Now()
	type waiting struct {
		row  map[string]any
		last time.Time
	}
	var found []waiting
	seen := map[string]bool{}
	add := func(m store.Message, mention *store.Message) {
		if seen[m.ChatJID] {
			return
		}
		chat, known := s.index.ChatInfo(ctx, session.InstanceID, m.ChatJID)
		if known && chat.Archived && !a.IncludeArchived {
			return
		}
		if known && chat.Muted(now) && !includeMuted {
			return
		}
		handled := false
		if mark, ok := marks[m.ChatJID]; ok && mark.Hides(m.SentAt, now) {
			if !a.IncludeHandled {
				return
			}
			handled = true
		}
		seen[m.ChatJID] = true
		count, first, _ := s.index.Waiting(ctx, session.InstanceID, m.ChatJID)
		row := map[string]any{"chat_jid": m.ChatJID, "name": names.name(ctx, m.ChatJID), "group": m.IsGroup,
			"last_message": names.preview(ctx, m), "waiting_messages": count, "age_hours": roundHours(now.Sub(m.SentAt))}
		if !first.IsZero() {
			row["waiting_since"] = first
		}
		if mention != nil {
			row["mention"] = names.preview(ctx, *mention)
		}
		if known && chat.Muted(now) {
			row["muted"] = true
		}
		if handled {
			row["handled"] = true
		}
		found = append(found, waiting{row: row, last: m.SentAt})
	}
	for _, m := range last {
		if m.FromMe {
			continue
		}
		if m.IsGroup && !a.IncludeGroups {
			continue
		}
		if ignoreClosing && isClosing(m, own) {
			continue
		}
		if a.MinAgeHours > 0 && now.Sub(m.SentAt) < time.Duration(a.MinAgeHours*float64(time.Hour)) {
			continue
		}
		add(m, nil)
	}
	if a.IncludeGroupMentions && !a.IncludeGroups && len(own) > 0 {
		mentions, err := s.index.Mentions(ctx, session.InstanceID, own, "", since, 200)
		if err != nil {
			return toolError("could not read the mentions: %v", err)
		}
		for _, m := range mentions {
			if !m.IsGroup || seen[m.ChatJID] || !m.SentAt.After(s.index.LastSent(ctx, session.InstanceID, m.ChatJID)) {
				continue
			}
			lastRow := m
			for _, r := range last {
				if r.ChatJID == m.ChatJID {
					lastRow = r
					break
				}
			}
			mention := m
			add(lastRow, &mention)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].last.After(found[j].last) })
	n := defaultLimit(a.Limit, 30, 200)
	truncated := len(found) > n
	if truncated {
		found = found[:n]
	}
	rows := make([]map[string]any, 0, len(found))
	for _, f := range found {
		rows = append(rows, f.row)
	}
	result := map[string]any{"chats": rows, "count": len(rows), "truncated": truncated, "since": since,
		"note":            "a chat waits when its latest message came from the other side. mark_handled or snooze_chat take one off this list until someone writes in it again",
		"content_warning": UntrustedContent}
	if a.IncludeGroupMentions && len(own) == 0 {
		result["mentions_note"] = "the account's own number is not known yet (it shows once the account has sent a message), so group mentions could not be looked for"
	}
	return textResult(result, false)
}

func roundHours(d time.Duration) float64 {
	return float64(int(d.Hours()*10)) / 10
}

func (s *Server) listMentions(ctx context.Context, session Session, a arguments) map[string]any {
	since := time.Now().AddDate(0, 0, -30)
	if a.Since != "" {
		t, err := parseWhen(a.Since, "since")
		if err != nil {
			return toolError("%v", err)
		}
		since = t
	}
	own := s.ownIDs(ctx, session)
	if len(own) == 0 {
		return toolError("the account's own number is not known yet; it shows once the account has sent a message")
	}
	rows, err := s.index.Mentions(ctx, session.InstanceID, own, strings.TrimSpace(a.ChatJID), since, defaultLimit(a.Limit, 50, 200))
	if err != nil {
		return toolError("could not read the mentions: %v", err)
	}
	names := s.namer(session)
	names.fill(ctx, rows)
	sent := map[string]time.Time{}
	out := []map[string]any{}
	for _, m := range rows {
		last, ok := sent[m.ChatJID]
		if !ok {
			last = s.index.LastSent(ctx, session.InstanceID, m.ChatJID)
			sent[m.ChatJID] = last
		}
		answered := last.After(m.SentAt)
		if a.OnlyUnanswered && answered {
			continue
		}
		out = append(out, map[string]any{"message": m, "answered": answered})
	}
	return textResult(map[string]any{"mentions": out, "count": len(out), "since": since,
		"note":            "answered means the account wrote in that chat after the mention. Found by the people a message lists as mentioned and by the @number WhatsApp writes into its text",
		"content_warning": UntrustedContent}, false)
}

func (s *Server) markHandled(ctx context.Context, session Session, a arguments) map[string]any {
	chat := strings.TrimSpace(a.ChatJID)
	if chat == "" {
		return toolError("chat_jid is required")
	}
	if a.Clear {
		if err := s.index.ClearMark(ctx, session.InstanceID, chat); err != nil {
			return toolError("%v", err)
		}
		return textResult(map[string]any{"cleared": true, "chat_jid": chat}, false)
	}
	if err := s.index.MarkHandled(ctx, session.InstanceID, chat, strings.TrimSpace(a.Note), time.Now()); err != nil {
		return toolError("%v", err)
	}
	return textResult(map[string]any{"handled": true, "chat_jid": chat, "name": s.index.ChatName(ctx, session.InstanceID, chat),
		"note": "kept in the gateway only: nothing is sent and the other side sees nothing. A new message in the chat brings it back to the lists"}, false)
}

func (s *Server) snoozeChat(ctx context.Context, session Session, a arguments) map[string]any {
	chat := strings.TrimSpace(a.ChatJID)
	if chat == "" {
		return toolError("chat_jid is required")
	}
	until, err := parseWhen(a.Until, "until")
	if err != nil {
		return toolError("%v", err)
	}
	if until.IsZero() || !until.After(time.Now()) {
		return toolError("until must be a moment in the future")
	}
	if err := s.index.Snooze(ctx, session.InstanceID, chat, strings.TrimSpace(a.Note), time.Now(), until); err != nil {
		return toolError("%v", err)
	}
	return textResult(map[string]any{"snoozed": true, "chat_jid": chat, "name": s.index.ChatName(ctx, session.InstanceID, chat), "until": until,
		"note": "kept in the gateway only. The chat comes back to the lists at that moment, or earlier if someone writes in it"}, false)
}

// ownIDs are the JIDs the account is known by: the ones its own messages
// show, and the number Evolution reported on connecting, when the session is
// the instance the panel selected (the one the connection state is about).
func (s *Server) ownIDs(ctx context.Context, session Session) []string {
	ids := s.index.OwnIDs(ctx, session.InstanceID)
	if selected, err := s.index.SelectedInstance(ctx); err == nil && selected == session.InstanceID {
		if jid := s.state.Snapshot().WhatsApp.JID; jid != "" {
			user, server, _ := strings.Cut(jid, "@")
			user, _, _ = strings.Cut(user, ":")
			user, _, _ = strings.Cut(user, ".")
			if user != "" && server != "" {
				bare := user + "@" + server
				found := false
				for _, id := range ids {
					found = found || id == bare
				}
				if !found {
					ids = append(ids, bare)
				}
			}
		}
	}
	return ids
}
