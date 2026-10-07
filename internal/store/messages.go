package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Chat is one conversation as the index knows it. Evolution Go has no route to
// list conversations, so this view is derived from the messages that were
// ingested, and its coverage is exactly the coverage of the index. The flags
// and the unread count come from chat_state: what history syncs, read
// receipts and the tools have said about the chat.
type Chat struct {
	ChatJID       string     `json:"chat_jid"`
	Name          string     `json:"name,omitempty"`
	IsGroup       bool       `json:"is_group"`
	Messages      int64      `json:"messages"`
	LastMessageAt time.Time  `json:"last_message_at,omitempty"`
	LastText      string     `json:"last_text,omitempty"`
	LastFromMe    bool       `json:"last_from_me"`
	UnreadCount   int64      `json:"unread_count"`
	MarkedUnread  bool       `json:"marked_unread,omitempty"`
	Archived      bool       `json:"archived,omitempty"`
	Pinned        bool       `json:"pinned,omitempty"`
	MutedUntil    *time.Time `json:"muted_until,omitempty"`
}

// Muted reports whether the chat is muted at now.
func (c Chat) Muted(now time.Time) bool { return c.MutedUntil != nil && c.MutedUntil.After(now) }

// MessageQuery bounds a read of the index. Every field is optional except the
// instance, which is never taken from the caller.
type MessageQuery struct {
	ChatJID    string
	Query      string
	Since      time.Time
	Until      time.Time
	Limit      int
	Oldest     bool
	MediaTypes []string
}

const maxPageSize = 500

func (q MessageQuery) limit() int {
	if q.Limit <= 0 {
		return 50
	}
	if q.Limit > maxPageSize {
		return maxPageSize
	}
	return q.Limit
}

// chatsQuery lists the conversations of one instance with their state:
// one row per conversation (a LID chat under its phone number when the
// pairing is known), the last visible message, the name (the group or
// contact name a history sync gave, else the push name most recently seen
// from the other side), the flags, and how many messages from others came
// after the moment the account read up to. $1 is the instance; the caller
// appends the filters, ordering and limit.
const chatsQuery = `
                WITH canonical AS (
                    SELECT coalesce(a.pn, m.chat_jid) AS chat_jid, m.is_group, m.text, m.media_type, m.from_me, m.sender_name, m.sent_at,
                           (` + visible + `) AS visible
                    FROM messages m
                    LEFT JOIN jid_aliases a ON a.instance_id = m.instance_id AND a.lid = m.chat_jid
                    WHERE m.instance_id = $1 /*chat*/
                ), states AS (
                    SELECT coalesce(a.pn, s.chat_jid) AS chat_jid, max(s.read_until) AS read_until, bool_or(s.marked_unread) AS marked_unread,
                           bool_or(s.archived) AS archived, bool_or(s.pinned) AS pinned, max(s.muted_until) AS muted_until,
                           max(nullif(s.name, '')) AS name, max(s.flags_at) AS flags_at
                    FROM chat_state s
                    LEFT JOIN jid_aliases a ON a.instance_id = s.instance_id AND a.lid = s.chat_jid
                    WHERE s.instance_id = $1
                    GROUP BY 1
                ), tracking AS (
                    SELECT coalesce((SELECT value::timestamptz FROM gateway_settings WHERE key = 'unread_tracking_since'), '-infinity'::timestamptz) AS since
                ), summary AS (
                    SELECT c.chat_jid, bool_or(c.is_group) AS is_group, count(*) FILTER (WHERE c.visible) AS total,
                           max(c.sent_at) FILTER (WHERE c.visible) AS last_at,
                           max(c.sent_at) FILTER (WHERE c.from_me) AS last_own_at,
                           max(c.sent_at) FILTER (WHERE c.visible AND NOT c.from_me) AS last_in_at,
                           coalesce(max(c.sender_name) FILTER (WHERE c.sender_name <> '' AND NOT c.from_me), '') AS display_name
                    FROM canonical c GROUP BY c.chat_jid
                ), last AS (
                    SELECT DISTINCT ON (c.chat_jid) c.chat_jid, c.text, c.from_me
                    FROM canonical c WHERE c.visible
                    ORDER BY c.chat_jid, c.sent_at DESC NULLS LAST
                ), unread AS (
                    SELECT c.chat_jid, count(*) AS n
                    FROM canonical c
                    JOIN summary s ON s.chat_jid = c.chat_jid
                    LEFT JOIN states st ON st.chat_jid = c.chat_jid
                    CROSS JOIN tracking t
                    WHERE c.visible AND NOT c.from_me
                      AND c.sent_at > GREATEST(coalesce(st.read_until, t.since), coalesce(s.last_own_at, '-infinity'::timestamptz))
                    GROUP BY c.chat_jid
                )
                SELECT s.chat_jid, s.is_group, s.total, s.last_at, coalesce(l.text, '') AS last_text, coalesce(l.from_me, FALSE) AS last_from_me,
                       CASE WHEN s.is_group THEN coalesce(st.name, '') ELSE coalesce(st.name, s.display_name) END AS name,
                       coalesce(u.n, 0) AS unread,
                       coalesce(st.marked_unread, FALSE) AS marked_unread,
                       -- WhatsApp takes a chat out of the archive when a message arrives.
                       (coalesce(st.archived, FALSE) AND (st.flags_at IS NULL OR s.last_in_at IS NULL OR s.last_in_at <= st.flags_at)) AS archived,
                       coalesce(st.pinned, FALSE) AS pinned, st.muted_until
                FROM summary s
                LEFT JOIN last l ON l.chat_jid = s.chat_jid
                LEFT JOIN states st ON st.chat_jid = s.chat_jid
                LEFT JOIN unread u ON u.chat_jid = s.chat_jid
                WHERE s.total > 0`

func scanChats(rows *sql.Rows) ([]Chat, error) {
	var chats []Chat
	for rows.Next() {
		var chat Chat
		var lastAt, muted sql.NullTime
		if err := rows.Scan(&chat.ChatJID, &chat.IsGroup, &chat.Messages, &lastAt, &chat.LastText, &chat.LastFromMe, &chat.Name,
			&chat.UnreadCount, &chat.MarkedUnread, &chat.Archived, &chat.Pinned, &muted); err != nil {
			return nil, err
		}
		if lastAt.Valid {
			chat.LastMessageAt = lastAt.Time.UTC()
		}
		if muted.Valid {
			t := muted.Time.UTC()
			chat.MutedUntil = &t
		}
		chats = append(chats, chat)
	}
	return chats, rows.Err()
}

// ListChats returns the conversations of one instance, pinned first, then
// most recently active. search matches the JID or the name.
func (s *Store) ListChats(ctx context.Context, instanceID string, search string, limit int) ([]Chat, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	if limit <= 0 || limit > maxPageSize {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT * FROM (`+chatsQuery+`) c
                WHERE ($2 = '' OR c.chat_jid ILIKE '%' || $2 || '%' OR c.name ILIKE '%' || $2 || '%')
                ORDER BY c.pinned DESC, c.last_at DESC NULLS LAST
                LIMIT $3`, instanceID, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChats(rows)
}

// UnreadChats lists the conversations with unread messages, or marked unread
// by hand, most recently active first.
func (s *Store) UnreadChats(ctx context.Context, instanceID string, includeArchived bool, limit int) ([]Chat, error) {
	if limit <= 0 || limit > maxPageSize {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT * FROM (`+chatsQuery+`) c
                WHERE (c.unread > 0 OR c.marked_unread) AND ($2 OR NOT c.archived)
                  AND c.chat_jid NOT LIKE '%@broadcast' AND c.chat_jid NOT LIKE '%@newsletter'
                ORDER BY c.last_at DESC NULLS LAST
                LIMIT $3`, instanceID, includeArchived, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChats(rows)
}

// ChatInfo is one conversation's row, found under any of its JIDs.
func (s *Store) ChatInfo(ctx context.Context, instanceID, chatJID string) (Chat, bool) {
	// Only this chat's messages are read, under any of its JIDs.
	query := strings.Replace(chatsQuery, "/*chat*/", "AND "+strings.ReplaceAll(sameChat, "?", "$2"), 1)
	rows, err := s.DB.QueryContext(ctx, `SELECT * FROM (`+query+`) c
                WHERE c.chat_jid = $2 OR c.chat_jid = (SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = $2) LIMIT 1`, instanceID, chatJID)
	if err != nil {
		return Chat{}, false
	}
	defer rows.Close()
	chats, err := scanChats(rows)
	if err != nil || len(chats) == 0 {
		return Chat{}, false
	}
	return chats[0], true
}

// Messages reads the index for one instance under the given bounds. Ordering is
// newest first by default, because that is what a caller asking "what was said"
// wants; Oldest flips it for a chronological read of a period.
//
// Every message carries its transcript when one exists, and a search matches
// the transcript as well as the text: a voice note someone transcribed is, for
// anyone reading the conversation, what was said.
func (s *Store) Messages(ctx context.Context, instanceID string, query MessageQuery) ([]Message, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	conditions := []string{"m.instance_id = $1"}
	args := []any{instanceID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(condition, "?", "$"+itoa(len(args))))
	}
	if query.ChatJID != "" {
		// A conversation WhatsApp moved to a LID lives under two JIDs; asking
		// for either one reads both.
		add(sameChat, query.ChatJID)
	}
	if query.Query != "" {
		add("(m.search_vector @@ websearch_to_tsquery('simple', ?) OR to_tsvector('simple', coalesce(t.text, '')) @@ websearch_to_tsquery('simple', ?))", query.Query)
	}
	if !query.Since.IsZero() {
		add("m.sent_at >= ?", query.Since)
	}
	if !query.Until.IsZero() {
		add("m.sent_at <= ?", query.Until)
	}
	if len(query.MediaTypes) > 0 {
		add("m.media_type = ANY(?)", pgTextArray(query.MediaTypes))
	}
	order := "DESC NULLS LAST"
	if query.Oldest {
		order = "ASC NULLS LAST"
	}
	args = append(args, query.limit())
	// A row with neither text, media nor a reaction is a protocol message
	// indexed by an older decoder: there is nothing in it to read.
	conditions = append(conditions, "(m.text <> '' OR m.media_type <> '' OR m.reaction_to <> '')")
	statement := `SELECT ` + messageColumns + `,coalesce(t.text,'')
                FROM messages m LEFT JOIN transcriptions t ON t.instance_id = m.instance_id AND t.message_id = m.message_id
                WHERE ` + strings.Join(conditions, " AND ") +
		` ORDER BY m.sent_at ` + order + ` LIMIT $` + itoa(len(args))
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows, true)
}

// messageColumns are the columns every message read selects, in the order
// scanRows reads them.
const messageColumns = `m.instance_id,m.message_id,m.chat_jid,m.sender_jid,m.sender_name,m.from_me,m.is_group,m.media_type,m.text,m.sent_at,
	m.quoted_id,m.mentions,m.forwarded,m.reaction_to,m.reaction,m.mime_type,m.filename,m.edited,m.revoked`

// visible is what counts as a message someone wrote: not a reaction, not
// deleted, and not an empty protocol row.
const visible = `m.reaction_to = '' AND NOT m.revoked AND (m.text <> '' OR m.media_type <> '')`

// scanRows reads rows selected with messageColumns, followed by the
// transcript when withTranscript is set.
func scanRows(rows *sql.Rows, withTranscript bool) ([]Message, error) {
	var result []Message
	for rows.Next() {
		var m Message
		var sent sql.NullTime
		var mentions string
		dest := []any{&m.InstanceID, &m.MessageID, &m.ChatJID, &m.SenderJID, &m.SenderName, &m.FromMe, &m.IsGroup, &m.MediaType, &m.Text, &sent,
			&m.QuotedID, &mentions, &m.Forwarded, &m.ReactionTo, &m.Reaction, &m.MimeType, &m.Filename, &m.Edited, &m.Revoked}
		if withTranscript {
			dest = append(dest, &m.Transcript)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if sent.Valid {
			m.SentAt = sent.Time.UTC()
		}
		if mentions != "" {
			m.Mentions = strings.Split(mentions, ",")
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// OldestMessage returns the earliest indexed message of a conversation. It is
// the anchor a history request pages backwards from: Evolution asks WhatsApp
// for the messages immediately before a message it already knows.
func (s *Store) OldestMessage(ctx context.Context, instanceID, chatJID string) (Message, error) {
	conditions, args := "instance_id = $1", []any{instanceID}
	if chatJID != "" {
		conditions, args = conditions+" AND chat_jid = $2", append(args, chatJID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`
                FROM messages m WHERE `+conditions+` AND sent_at IS NOT NULL ORDER BY sent_at ASC LIMIT 1`, args...)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(messages) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return messages[0], nil
}

// RawMessage returns the stored Evolution payload of one message, which is what
// Evolution needs back in order to decode its media.
func (s *Store) RawMessage(ctx context.Context, instanceID, messageID string) ([]byte, error) {
	var payload []byte
	err := s.DB.QueryRowContext(ctx, `SELECT e.payload FROM messages m JOIN events e ON e.event_id = m.event_id WHERE m.instance_id=$1 AND m.message_id=$2`, instanceID, messageID).Scan(&payload)
	return payload, err
}

func scanMessages(rows *sql.Rows) ([]Message, error) { return scanRows(rows, false) }

// pgTextArray renders a Go slice as a PostgreSQL text array literal, which is
// what ANY() expects from the lib/pq-style driver.
func pgTextArray(values []string) string {
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

func itoa(value int) string {
	digits := ""
	if value == 0 {
		return "0"
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// Gap is a window in which the index holds nothing. A quiet account and a dead
// pipeline look identical from the outside, so a gap is reported as a suspicion
// to be weighed rather than as proof that messages were lost: the surrounding
// timestamps are facts, the hole between them is not.
type Gap struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

// Silence is how long the whole index must go quiet before the hole is worth
// reporting. An account sleeps every night, so anything shorter than a day is
// ordinary; a window this wide across every conversation at once is not.
const Silence = 24 * time.Hour

// IndexGaps reports the windows in which no conversation of the instance
// produced a single message. It looks at the index as a whole rather than at
// one chat because one silent chat says nothing, while every chat falling
// silent at the same moment is the shape an outage leaves behind.
//
// Ordering is most recent first, since a caller repairing an index cares about
// the hole it is still living with.
func (s *Store) IndexGaps(ctx context.Context, instanceID string, silence time.Duration, limit int) ([]Gap, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	if silence <= 0 {
		silence = Silence
	}
	if limit <= 0 || limit > maxPageSize {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx, `
                WITH ordered AS (
                    SELECT sent_at, lag(sent_at) OVER (ORDER BY sent_at) AS previous
                    FROM messages
                    WHERE instance_id = $1 AND sent_at IS NOT NULL
                )
                SELECT previous, sent_at FROM ordered
                WHERE previous IS NOT NULL AND sent_at - previous > make_interval(secs => $2::double precision)
                ORDER BY sent_at DESC
                LIMIT $3`, instanceID, silence.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var gaps []Gap
	for rows.Next() {
		var gap Gap
		if err := rows.Scan(&gap.Since, &gap.Until); err != nil {
			return nil, err
		}
		gap.Since, gap.Until = gap.Since.UTC(), gap.Until.UTC()
		gaps = append(gaps, gap)
	}
	return gaps, rows.Err()
}

// GapAnchors returns, for each conversation, the earliest message indexed after
// the given moment. That message is the only handle a gap offers: WhatsApp
// answers a history request with the messages immediately *before* one it
// already knows, so refilling a hole means paging backwards from the first
// message that landed after it.
//
// A conversation with nothing after the hole therefore has no anchor at all and
// cannot be refilled, which is why the count of those is reported alongside
// rather than silently left out.
func (s *Store) GapAnchors(ctx context.Context, instanceID, chatJID string, after time.Time, limit int) ([]Message, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	if limit <= 0 || limit > maxPageSize {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `
                WITH ranked AS (
                    SELECT m.*, row_number() OVER (PARTITION BY chat_jid ORDER BY sent_at ASC) AS position
                    FROM messages m
                    WHERE instance_id = $1 AND sent_at > $2 AND ($3 = '' OR chat_jid = $3)
                )
                SELECT `+messageColumns+`
                FROM ranked m WHERE position = 1
                ORDER BY sent_at ASC
                LIMIT $4`, instanceID, after, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ChatsWithoutAnchor counts the conversations that hold nothing after the given
// moment. These are the conversations a gap repair cannot reach, and reporting
// how many there are keeps a partial repair from reading as a complete one.
func (s *Store) ChatsWithoutAnchor(ctx context.Context, instanceID string, after time.Time) (int64, error) {
	if instanceID == "" {
		return 0, errors.New("instance is required")
	}
	var total int64
	err := s.DB.QueryRowContext(ctx, `
                SELECT count(*) FROM (
                    SELECT chat_jid FROM messages
                    WHERE instance_id = $1 AND sent_at IS NOT NULL
                    GROUP BY chat_jid
                    HAVING max(sent_at) <= $2
                ) AS stale`, instanceID, after).Scan(&total)
	return total, err
}

// MessageByID returns one indexed message.
//
// It is what lets a destructive tool describe its target before acting: the
// caller names an opaque id, and the only way to show a human what that id
// actually refers to — which conversation, whose words, when — is to look it up
// first. It is also where authorship is settled, since the index records who
// sent a message and the caller's own claim about it cannot be trusted.
func (s *Store) MessageByID(ctx context.Context, instanceID, messageID string) (Message, error) {
	if instanceID == "" || messageID == "" {
		return Message{}, errors.New("an instance and a message are both required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+messageColumns+`
                FROM messages m WHERE instance_id = $1 AND message_id = $2 LIMIT 1`, instanceID, messageID)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(messages) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return messages[0], nil
}
