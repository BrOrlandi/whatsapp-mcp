package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Filter narrows the messages a count, a statistic or an export covers. The
// zero value covers every visible message of the instance.
type Filter struct {
	ChatJID string
	Since   time.Time
	Until   time.Time
	// Direction is "in" (received), "out" (sent by the account) or "".
	Direction string
	// MediaType is a media type (image, video, audio, document, sticker…),
	// "text" for messages without media, "any" for any media, or "".
	MediaType     string
	ExcludeGroups bool
}

// sameChat is the condition that matches a chat under any of its JIDs: the
// one asked for, and the LID or phone number paired with it.
//
// The JIDs are computed once, as an array, rather than in subqueries
// correlated with each message row, so the chat index serves the lookup.
const sameChat = `m.chat_jid = ANY(ARRAY(SELECT ?::text UNION SELECT lid FROM jid_aliases WHERE instance_id = $1 AND pn = ? UNION SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = ?))`

// where renders the filter for the instance in $1; the placeholders after it
// are numbered from 2.
func (f Filter) where(instanceID string) (string, []any) {
	clauses := []string{"m.instance_id = $1", visible}
	args := []any{instanceID}
	add := func(condition string, value any) {
		args = append(args, value)
		clauses = append(clauses, strings.ReplaceAll(condition, "?", "$"+itoa(len(args))))
	}
	if f.ChatJID != "" {
		add(sameChat, f.ChatJID)
	}
	if !f.Since.IsZero() {
		add("m.sent_at >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		add("m.sent_at < ?", f.Until)
	}
	switch f.Direction {
	case "in":
		clauses = append(clauses, "NOT m.from_me")
	case "out":
		clauses = append(clauses, "m.from_me")
	}
	switch f.MediaType {
	case "":
	case "text":
		clauses = append(clauses, "m.media_type IN ('', 'text')")
	case "any":
		clauses = append(clauses, "m.media_type NOT IN ('', 'text')")
	default:
		add("m.media_type = ?", f.MediaType)
	}
	if f.ExcludeGroups {
		clauses = append(clauses, "NOT m.is_group")
	}
	return strings.Join(clauses, " AND "), args
}

// CountMessages counts the messages a filter covers.
func (s *Store) CountMessages(ctx context.Context, instanceID string, f Filter) (int64, error) {
	where, args := f.where(instanceID)
	var n int64
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM messages m WHERE `+where, args...).Scan(&n)
	return n, err
}

// Bucket is one group of a message statistic.
type Bucket struct {
	Key   string    `json:"key"`
	Name  string    `json:"name,omitempty"`
	Count int64     `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// Stats counts messages grouped by chat, sender, day or month (days and
// months in the time zone named by zone, an IANA name). Chats and senders
// come busiest first, days and months most recent first. It returns the
// total the filter covers and how many groups exist, which can exceed limit.
func (s *Store) Stats(ctx context.Context, instanceID string, f Filter, groupBy, zone string, limit int) (buckets []Bucket, total int64, groups int, err error) {
	if zone == "" {
		zone = "UTC"
	}
	where, args := f.where(instanceID)
	args = append(args, zone)
	zoneArg := "$" + itoa(len(args))
	var key, order string
	switch groupBy {
	case "chat":
		key, order = "coalesce((SELECT pn FROM jid_aliases WHERE instance_id = m.instance_id AND lid = m.chat_jid), m.chat_jid)", "count(*) DESC"
	case "sender":
		bare := `regexp_replace(m.sender_jid, ':[0-9]+@', '@')`
		key, order = "CASE WHEN m.from_me THEN 'me' ELSE coalesce((SELECT pn FROM jid_aliases WHERE instance_id = m.instance_id AND lid = "+bare+"), "+bare+") END", "count(*) DESC"
	case "day":
		key, order = "to_char(m.sent_at AT TIME ZONE "+zoneArg+", 'YYYY-MM-DD')", "k DESC"
	case "month":
		key, order = "to_char(m.sent_at AT TIME ZONE "+zoneArg+", 'YYYY-MM')", "k DESC"
	default:
		return nil, 0, 0, errors.New("group_by must be chat, sender, day or month")
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT `+key+`) FROM messages m WHERE `+where+` AND `+zoneArg+` <> ''`, args...).Scan(&total, &groups); err != nil {
		return nil, 0, 0, err
	}
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+key+` AS k, count(*), min(m.sent_at), max(m.sent_at) FROM messages m WHERE `+where+` AND `+zoneArg+` <> ''
		GROUP BY k ORDER BY `+order+` LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var b Bucket
		var first, last sql.NullTime
		if err := rows.Scan(&b.Key, &b.Count, &first, &last); err != nil {
			return nil, 0, 0, err
		}
		b.First, b.Last = first.Time.UTC(), last.Time.UTC()
		buckets = append(buckets, b)
	}
	return buckets, total, groups, rows.Err()
}

// EachMessage calls fn for every message the filter covers, oldest first,
// with its transcript, streaming rather than loading them all.
func (s *Store) EachMessage(ctx context.Context, instanceID string, f Filter, fn func(Message) error) error {
	where, args := f.where(instanceID)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`,coalesce(t.text,'') FROM messages m
		LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
		WHERE `+where+` ORDER BY m.sent_at, m.message_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		page, err := scanOne(rows, true)
		if err != nil {
			return err
		}
		if err := fn(page); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanOne(rows *sql.Rows, withTranscript bool) (Message, error) {
	var m Message
	var sent sql.NullTime
	var mentions string
	dest := []any{&m.InstanceID, &m.MessageID, &m.ChatJID, &m.SenderJID, &m.SenderName, &m.FromMe, &m.IsGroup, &m.MediaType, &m.Text, &sent,
		&m.QuotedID, &mentions, &m.Forwarded, &m.ReactionTo, &m.Reaction, &m.MimeType, &m.Filename, &m.Edited, &m.Revoked}
	if withTranscript {
		dest = append(dest, &m.Transcript)
	}
	if err := rows.Scan(dest...); err != nil {
		return m, err
	}
	if sent.Valid {
		m.SentAt = sent.Time.UTC()
	}
	if mentions != "" {
		m.Mentions = strings.Split(mentions, ",")
	}
	return m, nil
}

// MessageContext returns up to before messages before the given one and up
// to after messages after it, in the same conversation, oldest first, with
// the message itself between them.
func (s *Store) MessageContext(ctx context.Context, instanceID string, target Message, before, after int) ([]Message, []Message, error) {
	read := func(op, order string, n int) ([]Message, error) {
		if n <= 0 {
			return nil, nil
		}
		where, args := Filter{ChatJID: target.ChatJID}.where(instanceID)
		args = append(args, target.SentAt, target.MessageID, n)
		k := len(args)
		rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`,coalesce(t.text,'') FROM messages m
			LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
			WHERE `+where+` AND (m.sent_at, m.message_id) `+op+` ($`+itoa(k-2)+`, $`+itoa(k-1)+`)
			ORDER BY m.sent_at `+order+`, m.message_id `+order+` LIMIT $`+itoa(k), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanRows(rows, true)
	}
	earlier, err := read("<", "DESC", before)
	if err != nil {
		return nil, nil, err
	}
	for i, j := 0, len(earlier)-1; i < j; i, j = i+1, j-1 {
		earlier[i], earlier[j] = earlier[j], earlier[i]
	}
	later, err := read(">", "ASC", after)
	return earlier, later, err
}

// MessageInChat returns one indexed message, preferring the conversation
// given when the same id exists in two.
func (s *Store) MessageInChat(ctx context.Context, instanceID, messageID, chatJID string) (Message, error) {
	if chatJID == "" {
		return s.MessageByID(ctx, instanceID, messageID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages m WHERE m.instance_id = $1 AND m.message_id = $2
		AND `+strings.ReplaceAll(sameChat, "?", "$3")+` LIMIT 1`, instanceID, messageID, chatJID)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	found, err := scanRows(rows, false)
	if err != nil {
		return Message{}, err
	}
	if len(found) == 0 {
		return s.MessageByID(ctx, instanceID, messageID)
	}
	return found[0], nil
}

// Incoming is the latest messages of one chat sent by others, newest first.
func (s *Store) Incoming(ctx context.Context, instanceID, chatJID string, n int) ([]Message, error) {
	where, args := Filter{ChatJID: chatJID, Direction: "in"}.where(instanceID)
	args = append(args, n)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`,coalesce(t.text,'') FROM messages m
		LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
		WHERE `+where+` ORDER BY m.sent_at DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows, true)
}

// LastMessages is the newest visible message of every chat active since a
// moment, newest chat first, a LID chat under its phone number: what decides
// whether a chat waits for a reply.
func (s *Store) LastMessages(ctx context.Context, instanceID string, since time.Time) ([]Message, error) {
	columns := strings.Replace(messageColumns, "m.chat_jid", "m.canonical_jid", 1)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+columns+`,coalesce(t.text,'') FROM (
			SELECT DISTINCT ON (coalesce(a.pn, m.chat_jid)) m.*, coalesce(a.pn, m.chat_jid) AS canonical_jid
			FROM messages m LEFT JOIN jid_aliases a ON a.instance_id = m.instance_id AND a.lid = m.chat_jid
			WHERE m.instance_id = $1 AND m.sent_at >= $2 AND `+visible+`
			ORDER BY coalesce(a.pn, m.chat_jid), m.sent_at DESC
		) m LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
		ORDER BY m.sent_at DESC`, instanceID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanRows(rows, true)
	return out, err
}

// Waiting counts what others wrote in a chat after the account's last
// message there, and when the first of it arrived.
func (s *Store) Waiting(ctx context.Context, instanceID, chatJID string) (int64, time.Time, error) {
	where, args := Filter{ChatJID: chatJID, Direction: "in"}.where(instanceID)
	last := s.LastSent(ctx, instanceID, chatJID)
	args = append(args, last)
	var count int64
	var first sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT count(*), min(m.sent_at) FROM messages m WHERE `+where+` AND m.sent_at > $`+itoa(len(args)), args...).Scan(&count, &first)
	return count, first.Time.UTC(), err
}

// LastSent is when the account last wrote in a chat; the zero time when it
// never did.
func (s *Store) LastSent(ctx context.Context, instanceID, chatJID string) time.Time {
	where, args := Filter{ChatJID: chatJID, Direction: "out"}.where(instanceID)
	var at sql.NullTime
	_ = s.DB.QueryRowContext(ctx, `SELECT max(m.sent_at) FROM messages m WHERE `+strings.Replace(where, visible, "TRUE", 1), args...).Scan(&at)
	if !at.Valid {
		return time.Time{}
	}
	return at.Time.UTC()
}

// Mentions finds messages from others that mention the account, newest
// first: by the JIDs a message lists as mentioned, or by the @number
// WhatsApp writes into its text. ids are the account's JIDs (phone number
// and LID).
func (s *Store) Mentions(ctx context.Context, instanceID string, ids []string, chatJID string, since time.Time, limit int) ([]Message, error) {
	where, args := Filter{ChatJID: chatJID, Since: since, Direction: "in"}.where(instanceID)
	var likes []string
	for _, id := range ids {
		user, _, _ := strings.Cut(id, "@")
		if user == "" {
			continue
		}
		args = append(args, id, "%@"+user+"%")
		likes = append(likes, `(','||m.mentions||',' LIKE '%,'||$`+itoa(len(args)-1)+`||',%' OR m.text LIKE $`+itoa(len(args))+`)`)
	}
	if len(likes) == 0 {
		return nil, nil
	}
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`,coalesce(t.text,'') FROM messages m
		LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
		WHERE `+where+` AND (`+strings.Join(likes, " OR ")+`) ORDER BY m.sent_at DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows, true)
}

// OwnIDs are the JIDs the account writes under, as its own messages show
// them: its phone number and, in groups that address people by LID, its LID.
func (s *Store) OwnIDs(ctx context.Context, instanceID string) []string {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT ON (split_part(sender_jid, '@', 2)) sender_jid FROM messages
		WHERE instance_id = $1 AND from_me AND sender_jid <> '' AND sender_jid <> chat_jid
		ORDER BY split_part(sender_jid, '@', 2), sent_at DESC`, instanceID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var jid string
		if rows.Scan(&jid) == nil {
			user, server, _ := strings.Cut(jid, "@")
			user, _, _ = strings.Cut(user, ":")
			if user != "" && (server == "s.whatsapp.net" || server == "lid") {
				ids = append(ids, user+"@"+server)
			}
		}
	}
	return ids
}

// Activity is how busy the index is: the newest message from someone else,
// and how many arrived in the last hour and day.
type Activity struct {
	NewestIncoming *time.Time `json:"newest_incoming,omitempty"`
	NewestAny      *time.Time `json:"newest_any,omitempty"`
	LastHour       int64      `json:"incoming_last_hour"`
	LastDay        int64      `json:"incoming_last_day"`
}

// Activity measures the index at now.
func (s *Store) Activity(ctx context.Context, instanceID string, now time.Time) (Activity, error) {
	var a Activity
	var incoming, any sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT max(sent_at) FILTER (WHERE NOT from_me), max(sent_at),
			count(*) FILTER (WHERE NOT from_me AND sent_at > $2::timestamptz - interval '1 hour'),
			count(*) FILTER (WHERE NOT from_me AND sent_at > $2::timestamptz - interval '1 day')
		FROM messages m WHERE instance_id = $1 AND sent_at <= $2::timestamptz + interval '1 minute' AND `+visible, instanceID, now).Scan(&incoming, &any, &a.LastHour, &a.LastDay)
	if incoming.Valid {
		t := incoming.Time.UTC()
		a.NewestIncoming = &t
	}
	if any.Valid {
		t := any.Time.UTC()
		a.NewestAny = &t
	}
	return a, err
}

// SetChatFlag records a change the account made to how a chat is shown:
// archived, pinned or muted (until a moment; the zero time unmutes).
func (s *Store) SetChatFlag(ctx context.Context, instanceID, chatJID, action string, mutedUntil time.Time) error {
	var set string
	switch action {
	case "archive":
		set = "archived = TRUE"
	case "unarchive":
		set = "archived = FALSE"
	case "pin":
		set = "pinned = TRUE"
	case "unpin":
		set = "pinned = FALSE"
	case "mute", "unmute":
		set = "muted_until = $3::timestamptz"
	default:
		return errors.New("unknown action " + action)
	}
	args := []any{instanceID, chatJID}
	if action == "mute" || action == "unmute" {
		args = append(args, nullableTime(mutedUntil))
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO chat_state (instance_id, chat_jid) VALUES ($1, `+canonicalChat+`)
		ON CONFLICT (instance_id, chat_jid) DO NOTHING`, instanceID, chatJID)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE chat_state SET `+set+`, flags_at = now(), updated_at = now() WHERE instance_id = $1 AND chat_jid = `+canonicalChat, args...)
	return err
}

// MarkChatRead records that the account read a chat up to a moment.
func (s *Store) MarkChatRead(ctx context.Context, instanceID, chatJID string, at time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := applyReads(ctx, tx, []ChatRead{{InstanceID: instanceID, ChatJID: chatJID, At: at}}); err != nil {
		return err
	}
	return tx.Commit()
}

// Mark is a chat the operator dealt with: handled at a moment, optionally
// snoozed until another. Both hide it from the triage lists until someone
// writes in it again.
type Mark struct {
	ChatJID      string     `json:"chat_jid"`
	HandledAt    time.Time  `json:"handled_at"`
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty"`
	Note         string     `json:"note,omitempty"`
}

// Hides reports whether the mark keeps a chat whose latest message from
// someone else is at last off the lists at now. A message after the mark
// brings the chat back; a snooze also lifts at its moment.
func (m Mark) Hides(last time.Time, now time.Time) bool {
	if last.After(m.HandledAt) {
		return false
	}
	if m.SnoozedUntil != nil {
		return now.Before(*m.SnoozedUntil)
	}
	return true
}

// Marks are the instance's marks by chat.
func (s *Store) Marks(ctx context.Context, instanceID string) (map[string]Mark, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT chat_jid, handled_at, snoozed_until, note FROM triage_marks WHERE instance_id = $1`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	marks := map[string]Mark{}
	for rows.Next() {
		var m Mark
		var snoozed sql.NullTime
		if err := rows.Scan(&m.ChatJID, &m.HandledAt, &snoozed, &m.Note); err != nil {
			return nil, err
		}
		m.HandledAt = m.HandledAt.UTC()
		if snoozed.Valid {
			t := snoozed.Time.UTC()
			m.SnoozedUntil = &t
		}
		marks[m.ChatJID] = m
	}
	return marks, rows.Err()
}

// MarkHandled records a chat as dealt with at a moment.
func (s *Store) MarkHandled(ctx context.Context, instanceID, chatJID, note string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO triage_marks (instance_id, chat_jid, handled_at, note) VALUES ($1, `+canonicalChat+`, $3, $4)
		ON CONFLICT (instance_id, chat_jid) DO UPDATE SET handled_at = EXCLUDED.handled_at, snoozed_until = NULL, note = EXCLUDED.note`,
		instanceID, chatJID, at, note)
	return err
}

// Snooze keeps a chat off the lists until a moment.
func (s *Store) Snooze(ctx context.Context, instanceID, chatJID, note string, at, until time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO triage_marks (instance_id, chat_jid, handled_at, snoozed_until, note) VALUES ($1, `+canonicalChat+`, $3, $4, $5)
		ON CONFLICT (instance_id, chat_jid) DO UPDATE SET handled_at = EXCLUDED.handled_at, snoozed_until = EXCLUDED.snoozed_until, note = EXCLUDED.note`,
		instanceID, chatJID, at, until, note)
	return err
}

// ClearMark forgets a chat's mark and snooze.
func (s *Store) ClearMark(ctx context.Context, instanceID, chatJID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM triage_marks WHERE instance_id = $1 AND chat_jid = `+canonicalChat, instanceID, chatJID)
	return err
}

// ChatOldest is the oldest indexed message time of a chat.
func (s *Store) ChatOldest(ctx context.Context, instanceID, chatJID string) (time.Time, error) {
	where, args := Filter{ChatJID: chatJID}.where(instanceID)
	var at sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT min(m.sent_at) FROM messages m WHERE `+where, args...).Scan(&at)
	return at.Time.UTC(), err
}

// ChatName is how a chat or a person is best known in the index: the name a
// history sync gave the chat, else the push name most recently seen from
// them; "" when the index has neither.
func (s *Store) ChatName(ctx context.Context, instanceID, jid string) string {
	if jid == "" {
		return ""
	}
	var name string
	err := s.DB.QueryRowContext(ctx, `SELECT coalesce(
			(SELECT nullif(name, '') FROM chat_state WHERE instance_id = $1 AND chat_jid = `+canonicalChat+` AND name <> '' LIMIT 1),
			(SELECT nullif(sender_name, '') FROM messages WHERE instance_id = $1 AND sender_jid = $2 AND NOT from_me AND sender_name <> ''
				ORDER BY sent_at DESC NULLS LAST LIMIT 1),
			(SELECT nullif(sender_name, '') FROM messages WHERE instance_id = $1 AND chat_jid = $2 AND NOT is_group AND NOT from_me AND sender_name <> ''
				ORDER BY sent_at DESC NULLS LAST LIMIT 1),
			'')`, instanceID, jid).Scan(&name)
	if err != nil {
		return ""
	}
	return name
}
