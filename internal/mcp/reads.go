package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// ---- compact reads ----

// messageFields are the keys a message can be trimmed to with fields.
var messageFields = []string{"message_id", "chat_jid", "chat_name", "sent_at", "from_me", "is_group", "sender_jid", "sender_name", "text",
	"media_type", "mime_type", "filename", "quoted_message_id", "mentions", "reaction_to", "reaction", "forwarded", "edited", "revoked",
	"transcript"}

// chatFields are the keys a chat of list_chats can be trimmed to.
var chatFields = []string{"chat_jid", "name", "is_group", "messages", "last_message_at", "last_text", "last_from_me", "unread_count",
	"marked_unread", "archived", "pinned", "muted_until"}

// shaping is how a bulk read is trimmed to what the caller needs: only some
// fields, and long texts cut. A trimmed page costs a fraction of the context.
type shaping struct {
	fields   []string
	maxChars int
}

func newShaping(a arguments, allowed []string) (shaping, error) {
	for _, f := range a.Fields {
		if !slices.Contains(allowed, f) {
			return shaping{}, fmt.Errorf("unknown field %q; the fields are %s", f, strings.Join(allowed, ", "))
		}
	}
	if a.MaxContentChars < 0 {
		return shaping{}, fmt.Errorf("max_content_chars must be positive")
	}
	return shaping{fields: a.Fields, maxChars: a.MaxContentChars}, nil
}

// clip shortens a text to n runes, marking the cut.
func clip(v string, n int) string {
	r := []rune(v)
	if n <= 0 || len(r) <= n {
		return v
	}
	return string(r[:n]) + "…"
}

// cut shortens long texts in place, marking each message it cut.
func (sh shaping) cut(msgs []store.Message) {
	if sh.maxChars <= 0 {
		return
	}
	for i := range msgs {
		if t := clip(msgs[i].Text, sh.maxChars); t != msgs[i].Text {
			msgs[i].Text, msgs[i].TextTruncated = t, true
		}
		if t := clip(msgs[i].Transcript, sh.maxChars); t != msgs[i].Transcript {
			msgs[i].Transcript, msgs[i].TextTruncated = t, true
		}
	}
}

// messages returns the messages as they go out: cut, then trimmed to fields.
func (sh shaping) messages(msgs []store.Message) any {
	if msgs == nil {
		msgs = []store.Message{}
	}
	sh.cut(msgs)
	if len(sh.fields) == 0 {
		return msgs
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, sh.pick(m))
	}
	return out
}

func (sh shaping) message(m store.Message) any {
	one := []store.Message{m}
	sh.cut(one)
	if len(sh.fields) == 0 {
		return one[0]
	}
	return sh.pick(one[0])
}

func (sh shaping) chats(chats []store.Chat) any {
	if chats == nil {
		chats = []store.Chat{}
	}
	if len(sh.fields) == 0 {
		return chats
	}
	out := make([]map[string]any, 0, len(chats))
	for _, c := range chats {
		out = append(out, sh.pick(c))
	}
	return out
}

// pick keeps the chosen keys of a value, and the truncation mark, which says
// the text is not whole.
func (sh shaping) pick(v any) map[string]any {
	body, _ := json.Marshal(v)
	var full map[string]any
	_ = json.Unmarshal(body, &full)
	out := map[string]any{}
	for _, f := range append(sh.fields, "text_truncated") {
		if val, ok := full[f]; ok {
			out[f] = val
		}
	}
	return out
}

// ---- names ----

// namer names chats and senders for one call, asking the index once per JID.
type namer struct {
	s        *Server
	instance string
	cache    map[string]string
}

func (s *Server) namer(session Session) *namer {
	return &namer{s: s, instance: session.InstanceID, cache: map[string]string{}}
}

func (n *namer) name(ctx context.Context, jid string) string {
	if jid == "" {
		return ""
	}
	if v, ok := n.cache[jid]; ok {
		return v
	}
	v := n.s.index.ChatName(ctx, n.instance, jid)
	n.cache[jid] = v
	return v
}

// fill names the chat of each message, and the sender where the row has no
// push name.
func (n *namer) fill(ctx context.Context, msgs []store.Message) {
	for i := range msgs {
		msgs[i].ChatName = n.name(ctx, msgs[i].ChatJID)
		if !msgs[i].FromMe && msgs[i].SenderName == "" {
			msgs[i].SenderName = n.name(ctx, msgs[i].SenderJID)
		}
	}
}

// nameGroups fills the names of groups the index has none for, live from
// WhatsApp, in one call. The message stream carries a group's participants'
// names, never the group's own.
func (s *Server) nameGroups(ctx context.Context, session Session, chats []store.Chat) {
	missing := false
	for _, c := range chats {
		missing = missing || (c.IsGroup && c.Name == "")
	}
	if !missing {
		return
	}
	groups, err := s.live.Groups(ctx, session.Token)
	if err != nil {
		return
	}
	names := map[string]string{}
	for _, g := range groups {
		names[g.JID] = g.Name
	}
	for i := range chats {
		if chats[i].IsGroup && chats[i].Name == "" {
			chats[i].Name = names[chats[i].ChatJID]
		}
	}
}

// ---- filters ----

// parseWhen reads a moment: RFC 3339, or a date, taken as midnight in the
// gateway's time zone.
func parseWhen(value, name string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%s must be an RFC 3339 time such as 2026-09-01T00:00:00Z, or a date such as 2026-09-01", name)
}

func filterOf(a arguments) (store.Filter, error) {
	f := store.Filter{ChatJID: strings.TrimSpace(a.ChatJID), ExcludeGroups: a.ExcludeGroups, MediaType: strings.TrimSpace(a.MediaType)}
	var err error
	if f.Since, err = parseWhen(a.Since, "since"); err != nil {
		return f, err
	}
	if f.Until, err = parseWhen(a.Until, "until"); err != nil {
		return f, err
	}
	switch a.Direction {
	case "", "all":
	case "in", "received":
		f.Direction = "in"
	case "out", "sent":
		f.Direction = "out"
	default:
		return f, fmt.Errorf("direction must be in or out")
	}
	return f, nil
}

// ---- tools ----

func (s *Server) listChats(ctx context.Context, session Session, args arguments) map[string]any {
	sh, err := newShaping(args, chatFields)
	if err != nil {
		return toolError("%v", err)
	}
	chats, err := s.index.ListChats(ctx, session.InstanceID, strings.TrimSpace(args.Search), args.Limit)
	if err != nil {
		return toolError("could not read the conversations: %v", err)
	}
	s.nameGroups(ctx, session, chats)
	return s.readResult(ctx, session, map[string]any{"chats": sh.chats(chats), "count": len(chats)})
}

func (s *Server) chatMessages(ctx context.Context, session Session, args arguments) map[string]any {
	if args.ChatJID == "" {
		return toolError("chat_jid is required; list_chats returns the available ones")
	}
	sh, err := newShaping(args, messageFields)
	if err != nil {
		return toolError("%v", err)
	}
	query := store.MessageQuery{ChatJID: args.ChatJID, Limit: args.Limit, Oldest: args.Order == "oldest"}
	if query.Since, err = parseMoment(args.Since); err != nil {
		return toolError("since is not a valid RFC 3339 timestamp: %v", err)
	}
	if query.Until, err = parseMoment(args.Until); err != nil {
		return toolError("until is not a valid RFC 3339 timestamp: %v", err)
	}
	if args.CountOnly {
		// The listing reads until inclusively; the count matches it.
		until := query.Until
		if !until.IsZero() {
			until = until.Add(time.Microsecond)
		}
		n, err := s.index.CountMessages(ctx, session.InstanceID, store.Filter{ChatJID: args.ChatJID, Since: query.Since, Until: until})
		if err != nil {
			return toolError("could not count the messages: %v", err)
		}
		payload := map[string]any{"chat_jid": args.ChatJID, "chat_name": s.index.ChatName(ctx, session.InstanceID, args.ChatJID), "count": n,
			"note": "counts messages, not reactions or deleted ones"}
		if oldest, err := s.index.ChatOldest(ctx, session.InstanceID, args.ChatJID); err == nil && !oldest.IsZero() {
			payload["history_since"] = oldest
		}
		return s.readResult(ctx, session, payload)
	}
	messages, err := s.index.Messages(ctx, session.InstanceID, query)
	if err != nil {
		return toolError("could not read the messages: %v", err)
	}
	s.namer(session).fill(ctx, messages)
	payload := map[string]any{"messages": sh.messages(messages), "count": len(messages), "chat_jid": args.ChatJID}
	// An empty period is the one answer that must never be reported bare: a
	// conversation that was quiet and one the gateway failed to ingest look
	// identical here, and only the second is a lie worth catching.
	if len(messages) == 0 {
		if gap, found := s.gapOver(ctx, session, query.Since, query.Until); found {
			payload["gap"] = gap
			payload["warning"] = "This period falls inside a window where the index holds no message from any conversation, so it is unknown rather than empty. Call sync_history with before set to the end of this window before concluding nothing was said."
		}
	}
	return s.readResult(ctx, session, payload)
}

func (s *Server) searchMessages(ctx context.Context, session Session, args arguments) map[string]any {
	if strings.TrimSpace(args.Query) == "" {
		return toolError("query is required")
	}
	sh, err := newShaping(args, messageFields)
	if err != nil {
		return toolError("%v", err)
	}
	query := store.MessageQuery{Query: args.Query, ChatJID: args.ChatJID, Limit: args.Limit}
	if query.Since, err = parseMoment(args.Since); err != nil {
		return toolError("since is not a valid RFC 3339 timestamp: %v", err)
	}
	if query.Until, err = parseMoment(args.Until); err != nil {
		return toolError("until is not a valid RFC 3339 timestamp: %v", err)
	}
	messages, err := s.index.Messages(ctx, session.InstanceID, query)
	if err != nil {
		return toolError("could not search the messages: %v", err)
	}
	s.namer(session).fill(ctx, messages)
	return s.readResult(ctx, session, map[string]any{"messages": sh.messages(messages), "count": len(messages)})
}

func (s *Server) messageContext(ctx context.Context, session Session, args arguments) map[string]any {
	sh, err := newShaping(args, messageFields)
	if err != nil {
		return toolError("%v", err)
	}
	m, failure := s.targetIn(ctx, session, args.MessageID, args.ChatJID)
	if failure != nil {
		return failure
	}
	around := func(v *int) int {
		if v == nil {
			return 5
		}
		return min(max(*v, 0), 50)
	}
	before, after := around(args.beforeCount()), around(args.After)
	earlier, later, err := s.index.MessageContext(ctx, session.InstanceID, m, before, after)
	if err != nil {
		return toolError("could not read the conversation around %s: %v", m.MessageID, err)
	}
	names := s.namer(session)
	names.fill(ctx, earlier)
	names.fill(ctx, later)
	one := []store.Message{m}
	names.fill(ctx, one)
	s.attachTranscripts(ctx, session, one)
	return s.readResult(ctx, session, map[string]any{"chat_jid": m.ChatJID, "chat_name": names.name(ctx, m.ChatJID),
		"before": sh.messages(earlier), "message": sh.message(one[0]), "after": sh.messages(later)})
}

// attachTranscripts fills the transcript of voice notes read one by one,
// which the bulk queries join in themselves.
func (s *Server) attachTranscripts(ctx context.Context, session Session, msgs []store.Message) {
	if s.transcriber == nil {
		return
	}
	for i := range msgs {
		if msgs[i].MediaType == "audio" && msgs[i].Transcript == "" {
			if t, err := s.transcriber.Stored(ctx, session.InstanceID, msgs[i].MessageID); err == nil {
				msgs[i].Transcript = t.Text
			}
		}
	}
}

func (s *Server) messageStats(ctx context.Context, session Session, args arguments) map[string]any {
	f, err := filterOf(args)
	if err != nil {
		return toolError("%v", err)
	}
	groupBy := args.GroupBy
	if groupBy == "" {
		groupBy = "chat"
	}
	limit := args.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	zone := time.Local.String()
	if zone == "Local" {
		zone = "UTC"
	}
	buckets, total, groups, err := s.index.Stats(ctx, session.InstanceID, f, groupBy, zone, limit)
	if err != nil {
		return toolError("%v", err)
	}
	names := s.namer(session)
	switch groupBy {
	case "chat":
		for i := range buckets {
			buckets[i].Name = names.name(ctx, buckets[i].Key)
		}
	case "sender":
		for i := range buckets {
			if buckets[i].Key == "me" {
				buckets[i].Name = "the account itself"
			} else {
				buckets[i].Name = names.name(ctx, buckets[i].Key)
			}
		}
	}
	if buckets == nil {
		buckets = []store.Bucket{}
	}
	result := map[string]any{"group_by": groupBy, "buckets": buckets, "total": total, "groups": groups,
		"truncated": groups > len(buckets), "note": "counts messages, not reactions or deleted ones"}
	if groupBy == "day" || groupBy == "month" {
		result["timezone"] = zone
	}
	return textResult(result, false)
}

func (s *Server) exportMessages(ctx context.Context, session Session, args arguments) map[string]any {
	f, err := filterOf(args)
	if err != nil {
		return toolError("%v", err)
	}
	if s.exportDir == "" {
		return toolError("this gateway has no folder for exports configured (EXPORT_DIR)")
	}
	if s.publicURL == "" {
		return toolError("this gateway has no public URL configured, so it cannot hand out the export")
	}
	if err := os.MkdirAll(s.exportDir, 0o700); err != nil {
		return toolError("%v", err)
	}
	scope := "todas"
	if f.ChatJID != "" {
		scope = safeName(strings.SplitN(f.ChatJID, "@", 2)[0])
	}
	path := filepath.Join(s.exportDir, fmt.Sprintf("whatsapp-%s-%s.ndjson", scope, time.Now().Format("20060102-150405")))
	partial := path + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return toolError("%v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	w := bufio.NewWriterSize(file, 256<<10)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	names := s.namer(session)
	var count int
	var first, last time.Time
	err = s.index.EachMessage(ctx, session.InstanceID, f, func(m store.Message) error {
		one := []store.Message{m}
		names.fill(ctx, one)
		if count == 0 {
			first = m.SentAt
		}
		last = m.SentAt
		count++
		return enc.Encode(one[0])
	})
	if err == nil {
		err = w.Flush()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(partial, path)
	}
	if err != nil {
		_ = os.Remove(partial)
		return toolError("the export failed: %v", err)
	}
	info, _ := os.Stat(path)
	token, expires, err := s.links.createFile(session.InstanceID, path, "application/x-ndjson")
	if err != nil {
		return toolError("could not create a download link: %v", err)
	}
	url := s.publicURL + "/media/" + token
	result := map[string]any{"file": filepath.Base(path), "format": "ndjson", "count": count, "bytes": info.Size(),
		"url": url, "expires_at": expires.UTC(), "curl": "curl -fsSL -o " + filepath.Base(path) + " '" + url + "'",
		"note": "one JSON message per line, oldest first, reactions and deleted messages left out. The file stays on the gateway's server until it is deleted from the panel (Configurações › Arquivos baixados); the URL needs no credential and stops working at expires_at, and export_messages makes a new one."}
	if count > 0 {
		result["first_timestamp"], result["last_timestamp"] = first, last
	}
	return textResult(result, false)
}

// sortByTime orders messages oldest first.
func sortByTime(msgs []store.Message) {
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].SentAt.Before(msgs[j].SentAt) })
}
