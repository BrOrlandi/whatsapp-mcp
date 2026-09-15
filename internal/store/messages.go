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
// ingested, and its coverage is exactly the coverage of the index.
type Chat struct {
	ChatJID       string    `json:"chat_jid"`
	Name          string    `json:"name,omitempty"`
	IsGroup       bool      `json:"is_group"`
	Messages      int64     `json:"messages"`
	LastMessageAt time.Time `json:"last_message_at,omitempty"`
	LastText      string    `json:"last_text,omitempty"`
	LastFromMe    bool      `json:"last_from_me"`
}

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

// ListChats returns the conversations of one instance, most recently active
// first. The name is the push name most recently seen from the other side,
// which is the only name the message stream carries; group names come from
// Evolution and are filled in by the caller.
func (s *Store) ListChats(ctx context.Context, instanceID string, search string, limit int) ([]Chat, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	if limit <= 0 || limit > maxPageSize {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `
                WITH ranked AS (
                    SELECT chat_jid, is_group, text, from_me, sender_name, sent_at,
                           row_number() OVER (PARTITION BY chat_jid ORDER BY sent_at DESC NULLS LAST) AS position,
                           count(*) OVER (PARTITION BY chat_jid) AS total,
                           max(sent_at) OVER (PARTITION BY chat_jid) AS last_at
                    FROM messages
                    WHERE instance_id = $1
                )
                SELECT chat_jid, is_group, total, last_at, text, from_me,
                       coalesce(max(sender_name) FILTER (WHERE sender_name <> '' AND NOT from_me) OVER (PARTITION BY chat_jid), '') AS display_name
                FROM ranked
                WHERE position = 1 AND ($2 = '' OR chat_jid ILIKE '%' || $2 || '%')
                ORDER BY last_at DESC NULLS LAST
                LIMIT $3`, instanceID, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []Chat
	for rows.Next() {
		var chat Chat
		var lastAt sql.NullTime
		if err := rows.Scan(&chat.ChatJID, &chat.IsGroup, &chat.Messages, &lastAt, &chat.LastText, &chat.LastFromMe, &chat.Name); err != nil {
			return nil, err
		}
		if lastAt.Valid {
			chat.LastMessageAt = lastAt.Time.UTC()
		}
		chats = append(chats, chat)
	}
	return chats, rows.Err()
}

// Messages reads the index for one instance under the given bounds. Ordering is
// newest first by default, because that is what a caller asking "what was said"
// wants; Oldest flips it for a chronological read of a period.
func (s *Store) Messages(ctx context.Context, instanceID string, query MessageQuery) ([]Message, error) {
	if instanceID == "" {
		return nil, errors.New("instance is required")
	}
	conditions := []string{"instance_id = $1"}
	args := []any{instanceID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.Replace(condition, "?", "$"+itoa(len(args)), 1))
	}
	if query.ChatJID != "" {
		add("chat_jid = ?", query.ChatJID)
	}
	if query.Query != "" {
		add("search_vector @@ websearch_to_tsquery('simple', ?)", query.Query)
	}
	if !query.Since.IsZero() {
		add("sent_at >= ?", query.Since)
	}
	if !query.Until.IsZero() {
		add("sent_at <= ?", query.Until)
	}
	if len(query.MediaTypes) > 0 {
		add("media_type = ANY(?)", pgTextArray(query.MediaTypes))
	}
	order := "DESC NULLS LAST"
	if query.Oldest {
		order = "ASC NULLS LAST"
	}
	args = append(args, query.limit())
	statement := `SELECT instance_id,message_id,chat_jid,sender_jid,sender_name,from_me,is_group,media_type,text,sent_at
                FROM messages WHERE ` + strings.Join(conditions, " AND ") +
		` ORDER BY sent_at ` + order + ` LIMIT $` + itoa(len(args))
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// OldestMessage returns the earliest indexed message of a conversation. It is
// the anchor a history request pages backwards from: Evolution asks WhatsApp
// for the messages immediately before a message it already knows.
func (s *Store) OldestMessage(ctx context.Context, instanceID, chatJID string) (Message, error) {
	conditions, args := "instance_id = $1", []any{instanceID}
	if chatJID != "" {
		conditions, args = conditions+" AND chat_jid = $2", append(args, chatJID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT instance_id,message_id,chat_jid,sender_jid,sender_name,from_me,is_group,media_type,text,sent_at
                FROM messages WHERE `+conditions+` AND sent_at IS NOT NULL ORDER BY sent_at ASC LIMIT 1`, args...)
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

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var result []Message
	for rows.Next() {
		var m Message
		var sent sql.NullTime
		if err := rows.Scan(&m.InstanceID, &m.MessageID, &m.ChatJID, &m.SenderJID, &m.SenderName, &m.FromMe, &m.IsGroup, &m.MediaType, &m.Text, &sent); err != nil {
			return nil, err
		}
		if sent.Valid {
			m.SentAt = sent.Time.UTC()
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

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
