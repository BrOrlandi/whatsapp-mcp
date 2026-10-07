package webhook

import (
	"context"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
)

// Names names chats and people for the events, the way the tools do.
type Names interface {
	ChatName(ctx context.Context, instanceID, jid string) string
}

// FromEvent turns one decoded Evolution event into what webhooks receive:
// a live message (received, or sent by the account from any device or by a
// tool), a reaction, an edit or deletion of an earlier message, or a receipt.
// History syncs bring old messages and are not delivered.
func FromEvent(ctx context.Context, decoded events.Event, names Names) []Event {
	instance := decoded.Record.InstanceID
	now := time.Now().UTC()
	switch decoded.Kind {
	case events.KindReceipt:
		r := decoded.Receipt
		if r == nil || len(r.MessageIDs) == 0 {
			return nil
		}
		kind := r.Type
		if kind == "" {
			kind = "delivered"
		}
		return []Event{{Kind: "receipt", ID: randomHex(8), At: now, InstanceID: instance, chat: r.ChatJID,
			Receipt: &Receipt{ChatJID: r.ChatJID, SenderJID: r.SenderJID, MessageIDs: r.MessageIDs, Type: kind, Timestamp: r.Timestamp}}}
	case events.KindMessage:
	default:
		return nil
	}
	d, live := decoded.Details, decoded.Live
	if d == nil || live == nil || live.MessageID == "" {
		return nil
	}
	id, at := live.MessageID, live.SentAt
	switch {
	case d.RevokeOf != "":
		id = d.RevokeOf
	case d.EditOf != "":
		id = d.EditOf
	}
	if at.IsZero() {
		at = now
	}
	m := &Message{ID: id, ChatJID: live.ChatJID, Group: live.IsGroup || strings.HasSuffix(live.ChatJID, "@g.us"), Timestamp: at.UTC(), FromMe: live.FromMe,
		SenderJID: live.SenderJID, Text: d.Text, Forwarded: d.Forwarded}
	if names != nil {
		m.ChatName = names.ChatName(ctx, instance, live.ChatJID)
	}
	if !live.FromMe {
		m.SenderName = live.SenderName
		if m.SenderName == "" && names != nil {
			m.SenderName = names.ChatName(ctx, instance, live.SenderJID)
		}
	}
	switch d.MediaType {
	case "image", "video", "audio", "document", "sticker":
		m.Media = &Media{Type: d.MediaType, MimeType: d.MimeType, Filename: d.Filename, Bytes: d.Bytes, Caption: d.Caption}
		m.Text = d.Caption
	}
	if d.QuotedID != "" {
		m.ReplyTo = &Reference{ID: d.QuotedID, SenderJID: d.QuotedParticipant, Text: d.QuotedText}
	}
	if d.Location != nil {
		m.Location = map[string]any{"latitude": d.Location.Latitude, "longitude": d.Location.Longitude, "name": d.Location.Name,
			"address": d.Location.Address, "live": d.Location.Live}
	}
	if d.Poll != nil {
		m.Poll = map[string]any{"question": d.Poll.Question, "options": d.Poll.Options, "max_answers": d.Poll.MaxAnswers}
	}
	ev := Event{Kind: "message", ID: randomHex(8), At: now, InstanceID: instance, Message: m, chat: live.ChatJID, fromMe: live.FromMe}
	switch {
	case d.ReactionTo != "":
		ev.Kind = "reaction"
		m.Reaction = &Reaction{To: d.ReactionTo, Emoji: d.Reaction}
	case d.RevokeOf != "":
		m.Revoked, m.Text = true, ""
	case d.EditOf != "":
		m.Edited, m.Text = true, d.EditText
	case m.Text == "" && m.Media == nil && m.Location == nil && m.Poll == nil:
		return nil // a protocol message, a poll vote, a call: nothing to read
	}
	return []Event{ev}
}
