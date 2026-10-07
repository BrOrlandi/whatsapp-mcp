package mcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// resolveRecipient turns what a send was given (a JID, a phone number or a
// name) into the chat it reaches, as a JID. A name matching several chats
// returns them all, since guessing between them would send to the wrong
// person. Groups are matched by their name, live from WhatsApp.
func (s *Server) resolveRecipient(ctx context.Context, session Session, to string) (string, []map[string]any, error) {
	to = strings.TrimSpace(to)
	if strings.Contains(to, "@") {
		return to, nil, nil
	}
	if digits := phoneDigits(to); digits != "" {
		return digits + "@s.whatsapp.net", nil, nil
	}
	chats, err := s.index.ListChats(ctx, session.InstanceID, to, 6)
	if err != nil {
		return "", nil, err
	}
	seen := map[string]bool{}
	candidates := []map[string]any{}
	for _, c := range chats {
		if c.IsGroup {
			continue // a group's name comes from WhatsApp below
		}
		seen[c.ChatJID] = true
		candidates = append(candidates, map[string]any{"chat_jid": c.ChatJID, "name": c.Name})
	}
	if groups, err := s.live.Groups(ctx, session.Token); err == nil {
		for _, g := range groups {
			if !seen[g.JID] && matches(to, g.Name) {
				seen[g.JID] = true
				candidates = append(candidates, map[string]any{"chat_jid": g.JID, "name": g.Name})
			}
		}
	}
	switch len(candidates) {
	case 0:
		return "", nil, fmt.Errorf("no conversation or group is called %q; use a JID or a phone number with country code", to)
	case 1:
		return candidates[0]["chat_jid"].(string), nil, nil
	}
	return "", candidates, nil
}

// phoneDigits returns the digits of a phone number written with the usual
// punctuation, or "" when the text is not one.
func phoneDigits(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("+-() .", r):
		default:
			return ""
		}
	}
	if b.Len() < 8 {
		return ""
	}
	return b.String()
}

// recipient resolves a send's recipient, or the tool result that explains
// why it cannot. A phone number goes to Evolution as digits, which it
// formats itself (Brazil's ninth digit included).
func (s *Server) recipient(ctx context.Context, session Session, to string) (string, map[string]any) {
	if strings.TrimSpace(to) == "" {
		return "", toolError("to is required")
	}
	if digits := phoneDigits(strings.TrimSpace(to)); digits != "" {
		return digits, nil
	}
	jid, candidates, err := s.resolveRecipient(ctx, session, to)
	if err != nil {
		return "", toolError("%v", err)
	}
	if len(candidates) > 0 {
		return "", textResult(map[string]any{"sent": false, "recipient_candidates": candidates,
			"next": "nothing was sent. The name matches several chats: send to the JID of the right one"}, true)
	}
	return jid, nil
}

// dryRun answers a send asked as a draft: what would go, and to whom, with
// nothing sent.
func (s *Server) dryRun(ctx context.Context, session Session, to string, draft map[string]any) map[string]any {
	jid, candidates, err := s.resolveRecipient(ctx, session, to)
	if err != nil {
		return toolError("%v", err)
	}
	result := map[string]any{"dry_run": true, "sent": false, "draft": draft}
	if len(candidates) > 0 {
		result["recipient_candidates"] = candidates
		result["next"] = "nothing was sent. The name matches several chats, and sending to it would fail: send to the JID of the right one"
		return textResult(result, false)
	}
	recipient := map[string]any{"jid": jid}
	if name := s.index.ChatName(ctx, session.InstanceID, jid); name != "" {
		recipient["name"] = name
	}
	result["recipient"] = recipient
	if _, known := s.index.ChatInfo(ctx, session.InstanceID, jid); !known {
		result["note"] = "the index has no conversation with this recipient yet; check_numbers confirms that a number has WhatsApp"
	}
	result["next"] = "nothing was sent. Show the draft to the user and, once they approve it, call again without dry_run"
	return textResult(result, false)
}

// reply is a resolved reply_to: what makes Evolution quote the message, and
// what the quoted message says, for drafts.
type reply struct {
	id          string
	participant string
	quoted      map[string]any
}

// replyTo resolves the message a send quotes. WhatsApp quotes a message of
// the same conversation, so one found elsewhere is refused rather than sent
// as a reply that would not show.
func (s *Server) replyTo(ctx context.Context, session Session, id, to string) (*reply, map[string]any) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	chat, _, _ := s.resolveRecipient(ctx, session, to)
	m, err := s.index.MessageInChat(ctx, session.InstanceID, id, chat)
	if err != nil {
		return nil, toolError("no indexed message has id %s; the reading tools return the ids the gateway knows", id)
	}
	if chat != "" && !s.sameChat(ctx, session, m.ChatJID, chat) {
		return nil, toolError("message %s belongs to %s, not to the conversation being sent to; a reply quotes a message of the same conversation", id, m.ChatJID)
	}
	if m.Revoked {
		return nil, toolError("message %s was deleted, so it cannot be quoted", id)
	}
	r := &reply{id: m.MessageID}
	if m.IsGroup && !m.FromMe && m.SenderJID != "" {
		r.participant = m.SenderJID
	}
	r.quoted = map[string]any{"message_id": m.MessageID, "from_me": m.FromMe, "sent_at": m.SentAt, "text": clip(m.Text, 200)}
	if !m.FromMe {
		r.quoted["sender"] = m.SenderName
	}
	if m.MediaType != "" && m.MediaType != "text" {
		r.quoted["media_type"] = m.MediaType
	}
	return r, nil
}

// sameChat reports whether two JIDs are one conversation of the session.
func (s *Server) sameChat(ctx context.Context, session Session, a, b string) bool {
	if a == b {
		return true
	}
	ca, okA := s.index.ChatInfo(ctx, session.InstanceID, a)
	cb, okB := s.index.ChatInfo(ctx, session.InstanceID, b)
	return okA && okB && ca.ChatJID == cb.ChatJID
}

// mentionJIDs turns the people a text mentions into the JIDs WhatsApp
// expects. WhatsApp highlights a mention only where the text says @<number>,
// so a mention the text does not place is refused instead of sent as a plain
// notification.
func mentionJIDs(text string, mentions []string) ([]string, error) {
	var jids []string
	for _, m := range mentions {
		m = strings.TrimSpace(m)
		user := m
		jid := m
		if u, _, ok := strings.Cut(m, "@"); ok {
			user, _, _ = strings.Cut(u, ":")
		} else {
			user = phoneDigits(m)
			jid = user + "@s.whatsapp.net"
		}
		if user == "" {
			return nil, fmt.Errorf("mention %q is neither a phone number with country code nor a JID", m)
		}
		if !strings.Contains(text, "@"+user) {
			return nil, fmt.Errorf("the text must contain @%s where the mention goes; WhatsApp shows it as the person's name", user)
		}
		jids = append(jids, jid)
	}
	return jids, nil
}

func (s *Server) sendText(ctx context.Context, session Session, args arguments) map[string]any {
	if args.To == "" || args.Text == "" {
		return toolError("to and text are both required")
	}
	mentions, err := mentionJIDs(args.Text, args.Mentions)
	if err != nil {
		return toolError("%v", err)
	}
	r, failure := s.replyTo(ctx, session, args.ReplyTo, args.To)
	if failure != nil {
		return failure
	}
	if args.DryRun {
		draft := map[string]any{"text": args.Text}
		if r != nil {
			draft["reply_to"] = r.quoted
		}
		if len(mentions) > 0 {
			draft["mentions"] = mentions
		}
		return s.dryRun(ctx, session, args.To, draft)
	}
	to, failure := s.recipient(ctx, session, args.To)
	if failure != nil {
		return failure
	}
	opts := evolution.SendOptions{Mentions: mentions}
	if r != nil {
		opts.QuotedID, opts.QuotedParticipant = r.id, r.participant
	}
	s.warm(ctx, session, to)
	sent, err := s.live.SendText(ctx, session.Token, to, args.Text, opts)
	if err != nil {
		return liveError(err)
	}
	payload := map[string]any{"to": to}
	if r != nil {
		payload["reply_to"] = r.id
	}
	return textResult(s.confirm(ctx, session, sent, payload), false)
}

func (s *Server) sendMedia(ctx context.Context, session Session, args arguments) map[string]any {
	if args.To == "" || args.URL == "" || args.Type == "" {
		return toolError("to, type and url are all required")
	}
	switch args.Type {
	case "image", "video", "audio", "document", "sticker":
	default:
		return toolError("type must be one of image, video, audio, document or sticker")
	}
	if args.Type == "sticker" && args.Caption != "" {
		return toolError("a sticker carries no caption")
	}
	fetchURL, err := s.mediaSource(args.URL)
	if err != nil {
		return toolError("%s", err.Error())
	}
	r, failure := s.replyTo(ctx, session, args.ReplyTo, args.To)
	if failure != nil {
		return failure
	}
	if args.DryRun {
		draft := map[string]any{"type": args.Type, "url": args.URL}
		if args.Caption != "" {
			draft["caption"] = args.Caption
		}
		if args.Filename != "" {
			draft["filename"] = args.Filename
		}
		if r != nil {
			draft["reply_to"] = r.quoted
		}
		return s.dryRun(ctx, session, args.To, draft)
	}
	to, failure := s.recipient(ctx, session, args.To)
	if failure != nil {
		return failure
	}
	var opts evolution.SendOptions
	if r != nil {
		opts.QuotedID, opts.QuotedParticipant = r.id, r.participant
	}
	s.warm(ctx, session, to)
	sent, err := s.live.SendMedia(ctx, session.Token, to, args.Type, fetchURL, args.Caption, args.Filename, opts)
	if err != nil {
		return liveError(err)
	}
	return textResult(s.confirm(ctx, session, sent, map[string]any{"to": to, "type": args.Type}), false)
}

// mediaSource is the URL Evolution fetches a file to send from. A link this
// gateway handed out (download_media, export_messages) is fetched from inside
// the stack, through the internal address, since the public one may not be
// reachable from there; any other URL must be a public one.
func (s *Server) mediaSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if s.publicURL != "" && strings.HasPrefix(raw, s.publicURL+"/media/") {
		token := strings.TrimPrefix(raw, s.publicURL+"/media/")
		if _, ok := s.links.resolve(token); !ok {
			return "", fmt.Errorf("this download link has expired; call download_media again for a new one")
		}
		return s.ownLink(token), nil
	}
	if err := checkMediaURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}

// ownLink is the address Evolution fetches one of this gateway's links at.
func (s *Server) ownLink(token string) string {
	base := s.internalURL
	if base == "" {
		base = s.publicURL
	}
	return base + "/media/" + url.PathEscape(token)
}

func (s *Server) forwardMessage(ctx context.Context, session Session, args arguments) map[string]any {
	if strings.TrimSpace(args.To) == "" {
		return toolError("to is required")
	}
	m, failure := s.targetIn(ctx, session, args.MessageID, args.ChatJID)
	if failure != nil {
		return failure
	}
	if m.Revoked {
		return toolError("message %s was deleted, so it cannot be forwarded", m.MessageID)
	}
	if m.ReactionTo != "" {
		return toolError("message %s is a reaction, which cannot be forwarded", m.MessageID)
	}
	if args.DryRun {
		draft := map[string]any{"forward": map[string]any{"message_id": m.MessageID, "from": s.index.ChatName(ctx, session.InstanceID, m.ChatJID),
			"chat_jid": m.ChatJID, "sent_at": m.SentAt, "text": clip(m.Text, 200), "media_type": m.MediaType, "filename": m.Filename}}
		return s.dryRun(ctx, session, args.To, draft)
	}
	to, failure := s.recipient(ctx, session, args.To)
	if failure != nil {
		return failure
	}
	opts := evolution.SendOptions{Forwarded: true}
	s.warm(ctx, session, to)
	var sent evolution.SentMessage
	var err error
	switch m.MediaType {
	case "", "text", "contact":
		if strings.TrimSpace(m.Text) == "" {
			return toolError("message %s has nothing to forward", m.MessageID)
		}
		sent, err = s.live.SendText(ctx, session.Token, to, m.Text, opts)
	case "image", "video", "audio", "document", "sticker":
		file, failure := s.mediaFile(ctx, session, m)
		if failure != nil {
			return failure
		}
		token, _, lerr := s.links.createFile(session.InstanceID, file.path, file.mimeType)
		if lerr != nil {
			return toolError("could not hand the file to WhatsApp: %v", lerr)
		}
		caption := ""
		if m.MediaType != "document" && m.MediaType != "sticker" {
			caption = m.Text
		}
		sent, err = s.live.SendMedia(ctx, session.Token, to, m.MediaType, s.ownLink(token), caption, m.Filename, opts)
	case "location":
		loc := locationOf(ctx, s, session, m)
		if loc == nil {
			return toolError("the location of message %s could not be read", m.MessageID)
		}
		sent, err = s.live.SendLocation(ctx, session.Token, to, loc.Latitude, loc.Longitude, loc.Name, loc.Address)
	default:
		return toolError("a %s message cannot be forwarded by the server version; only text, photos, videos, audios, documents, stickers and locations", m.MediaType)
	}
	if err != nil {
		return liveError(err)
	}
	return textResult(s.confirm(ctx, session, sent, map[string]any{"to": to, "forwarded": m.MessageID, "media_type": m.MediaType}), false)
}

func (s *Server) markChatRead(ctx context.Context, session Session, args arguments) map[string]any {
	chat := strings.TrimSpace(args.ChatJID)
	if chat == "" {
		return toolError("chat_jid is required")
	}
	receipts := args.Receipts == nil || *args.Receipts
	now := time.Now().UTC()
	result := map[string]any{"done": true, "chat_jid": chat, "receipts": receipts}
	if receipts {
		info, _ := s.index.ChatInfo(ctx, session.InstanceID, chat)
		n := int(info.UnreadCount)
		if n <= 0 {
			n = 1
		}
		rows, err := s.index.Incoming(ctx, session.InstanceID, chat, min(n, 100))
		if err != nil {
			return toolError("could not read the chat: %v", err)
		}
		if len(rows) > 0 {
			ids := make([]string, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.MessageID)
			}
			if err := s.live.MarkRead(ctx, session.Token, rows[0].ChatJID, ids); err != nil {
				return liveError(err)
			}
			result["messages"] = len(ids)
		}
	} else {
		result["note"] = "marked read in the gateway's lists only; WhatsApp was not told, so the phone still shows the chat unread"
	}
	if err := s.index.MarkChatRead(ctx, session.InstanceID, chat, now); err != nil {
		return toolError("%v", err)
	}
	return textResult(result, false)
}

func (s *Server) sendTyping(ctx context.Context, session Session, args arguments) map[string]any {
	to := strings.TrimSpace(args.To)
	if to == "" {
		return toolError("to is required")
	}
	typing := args.Typing == nil || *args.Typing
	if err := s.live.Presence(ctx, session.Token, to, typing, args.Audio && typing); err != nil {
		return liveError(err)
	}
	result := map[string]any{"done": true, "to": to, "typing": typing}
	if typing {
		result["note"] = "WhatsApp clears the indicator by itself after a few seconds, and when a message is sent"
	}
	return textResult(result, false)
}

// downloaded is a message's file on the gateway's disk.
type downloaded struct {
	path     string
	mimeType string
	bytes    int64
	cached   bool
}

// mediaFile returns a message's file: the copy already on disk, or one
// downloaded from WhatsApp now and kept.
func (s *Server) mediaFile(ctx context.Context, session Session, m store.Message) (downloaded, map[string]any) {
	if m.MediaType == "" || m.MediaType == "text" {
		return downloaded{}, toolError("message %q carries no media", m.MessageID)
	}
	if path, ok := s.cachedMedia(session.InstanceID, m.ChatJID, m.MessageID); ok {
		info, err := os.Stat(path)
		if err == nil {
			mimeType := m.MimeType
			if mimeType == "" {
				mimeType = mimeOfExt(filepath.Ext(path))
			}
			return downloaded{path: path, mimeType: mimeType, bytes: info.Size(), cached: true}, nil
		}
	}
	media, failure := s.media(ctx, session, m.MessageID)
	if failure != nil {
		return downloaded{}, failure
	}
	fallback := m.MimeType
	if fallback == "" {
		fallback = "application/octet-stream"
	}
	mimeType, data, err := decodeMedia(media, fallback)
	if err != nil {
		return downloaded{}, toolError("Evolution returned no usable media for message %q: %v", m.MessageID, err)
	}
	if strings.HasPrefix(mimeType, "application/octet-stream") && m.MimeType != "" {
		mimeType = m.MimeType
	}
	path, err := s.keepMedia(session.InstanceID, m.ChatJID, m.MessageID, mimeType, data)
	if err != nil {
		return downloaded{}, toolError("could not keep the file on the gateway: %v", err)
	}
	return downloaded{path: path, mimeType: mimeType, bytes: int64(len(data))}, nil
}
