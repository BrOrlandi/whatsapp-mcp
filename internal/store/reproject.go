package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// PendingEvent is one delivery whose messages are missing or unreadable.
type PendingEvent struct {
	ID      string
	Type    string
	Payload []byte
}

// UnprojectedEvents returns message deliveries that have no readable row.
//
// The condition is the absence of a *good* row rather than the presence of a
// bad one, which matters: a decoder that produced nothing at all — media
// without a caption, under the old decoder — left no orphan to find, and
// selecting on orphans alone would silently skip those deliveries.
func (s *Store) UnprojectedEvents(ctx context.Context, limit int) ([]PendingEvent, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := s.DB.QueryContext(ctx, `
                SELECT e.event_id, e.event_type, e.payload
                  FROM events e
                 WHERE e.event_type IN ('Message','SendMessage')
                   AND NOT EXISTS (
                       SELECT 1 FROM messages m
                        WHERE m.event_id = e.event_id
                          AND m.instance_id <> ''
                          AND m.sent_at IS NOT NULL)
                 ORDER BY e.received_at
                 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []PendingEvent
	for rows.Next() {
		var event PendingEvent
		if err := rows.Scan(&event.ID, &event.Type, &event.Payload); err != nil {
			return nil, err
		}
		pending = append(pending, event)
	}
	return pending, rows.Err()
}

// Reproject replaces the unreadable rows of one event with the ones a current
// decoder produced, in a single transaction.
//
// Rows that already carry an instance are left untouched. Re-deriving a row
// that was always correct would risk replacing good data with whatever the
// decoder does today, and the pass has to be safe to run twice.
func (s *Store) Reproject(ctx context.Context, eventID string, messages []Message) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE event_id = $1 AND (instance_id = '' OR chat_jid = '')`, eventID); err != nil {
		return 0, err
	}
	written := 0
	for _, m := range messages {
		if m.MessageID == "" || m.InstanceID == "" {
			continue
		}
		result, err := insertMessage(ctx, tx, m, eventID)
		if err != nil {
			return written, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			written += int(affected)
		}
	}
	return written, tx.Commit()
}

// OrphanRows counts the rows no query can reach: without an instance or a chat
// they are invisible to every read path, and they make a repaired window still
// read as empty.
func (s *Store) OrphanRows(ctx context.Context) (int64, error) {
	var total int64
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE instance_id = '' OR chat_jid = ''`).Scan(&total)
	return total, err
}

// DecoderVersion reports which decoder produced the current projection.
func (s *Store) DecoderVersion(ctx context.Context) (int, error) {
	var version int
	err := s.DB.QueryRowContext(ctx, `SELECT decoder_version FROM ingest_state WHERE singleton=TRUE`).Scan(&version)
	return version, err
}

// MarkReprojected records that the projection is current for a decoder, so the
// work is not repeated on every restart.
func (s *Store) MarkReprojected(ctx context.Context, version int, events, messages int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE ingest_state SET decoder_version=$1, reprojected_at=now(), reprojected_events=$2, reprojected_messages=$3 WHERE singleton=TRUE`,
		version, events, messages)
	return err
}

// LastReprojection describes the most recent pass, for the status surfaces.
func (s *Store) LastReprojection(ctx context.Context) (int, time.Time, int64, int64, error) {
	var version int
	var at *time.Time
	var events, messages int64
	err := s.DB.QueryRowContext(ctx, `SELECT decoder_version, reprojected_at, reprojected_events, reprojected_messages FROM ingest_state WHERE singleton=TRUE`).
		Scan(&version, &at, &events, &messages)
	when := time.Time{}
	if at != nil {
		when = at.UTC()
	}
	return version, when, events, messages, err
}

// insertMessage writes one message row with its details, leaving an existing
// row alone.
func insertMessage(ctx context.Context, tx execer, m Message, eventID string) (sql.Result, error) {
	return tx.ExecContext(ctx, `INSERT INTO messages (instance_id,message_id,chat_jid,sender_jid,sender_name,from_me,is_group,media_type,text,sent_at,event_id,
			quoted_id,mentions,forwarded,reaction_to,reaction,mime_type,filename,details_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT (instance_id,message_id) DO NOTHING`,
		m.InstanceID, m.MessageID, m.ChatJID, m.SenderJID, m.SenderName, m.FromMe, m.IsGroup, m.MediaType, m.Text, nullableTime(m.SentAt), eventID,
		m.QuotedID, strings.Join(m.Mentions, ","), m.Forwarded, m.ReactionTo, m.Reaction, m.MimeType, m.Filename, DetailsVersion)
}

// DetailsPending returns events whose message rows were written before the
// details columns existed, oldest first — so edits and chat flags are applied
// in the order they arrived — starting after the cursor (the zero cursor
// starts at the beginning).
func (s *Store) DetailsPending(ctx context.Context, after DetailsCursor, limit int) ([]PendingEvent, DetailsCursor, error) {
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT e.event_id, e.event_type, e.payload, e.received_at FROM events e
		WHERE (e.received_at, e.event_id) > ($3, $4)
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.event_id = e.event_id AND m.details_version < $1)
		ORDER BY e.received_at, e.event_id LIMIT $2`, DetailsVersion, limit, after.At, after.ID)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var pending []PendingEvent
	for rows.Next() {
		var event PendingEvent
		if err := rows.Scan(&event.ID, &event.Type, &event.Payload, &after.At); err != nil {
			return nil, after, err
		}
		after.ID = event.ID
		pending = append(pending, event)
	}
	return pending, after, rows.Err()
}

// DetailsCursor is where a pass over the pending events has got to.
type DetailsCursor struct {
	At time.Time
	ID string
}

// Enrich fills in the details of an event's rows from a current decoding of
// it: the columns the rows lacked, the edits and deletions it carries, and
// what a history sync says about each chat. A row the decoding no longer
// yields was a protocol message (an edit, a deletion) indexed as an empty
// message, and is removed.
func (s *Store) Enrich(ctx context.Context, eventID string, event Event) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	keep := make([]string, 0, len(event.Messages))
	for _, m := range event.Messages {
		keep = append(keep, m.MessageID)
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET quoted_id = $3, mentions = $4, forwarded = $5, reaction_to = $6, reaction = $7,
				mime_type = $8, filename = $9, media_type = CASE WHEN media_type = '' THEN $10 ELSE media_type END,
				text = CASE WHEN text = '' THEN $11 ELSE text END, details_version = $12
			WHERE instance_id = $1 AND message_id = $2 AND event_id = $13`,
			m.InstanceID, m.MessageID, m.QuotedID, strings.Join(m.Mentions, ","), m.Forwarded, m.ReactionTo, m.Reaction,
			m.MimeType, m.Filename, m.MediaType, m.Text, DetailsVersion, eventID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE event_id = $1 AND details_version < $2 AND NOT (message_id = ANY($3))
		AND text = '' AND media_type = ''`, eventID, DetailsVersion, pgTextArray(keep)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET details_version = $2 WHERE event_id = $1 AND details_version < $2`, eventID, DetailsVersion); err != nil {
		return err
	}
	if err := applyEdits(ctx, tx, event.Edits); err != nil {
		return err
	}
	// A replayed sync is a snapshot of long ago: its names and flags are
	// worth having, but its unread counts describe a past the account has
	// read since, and would count everything after it as unread.
	chats := make([]ChatState, 0, len(event.Chats))
	for _, c := range event.Chats {
		c.HasUnread = false
		chats = append(chats, c)
	}
	if err := applyChatStates(ctx, tx, chats); err != nil {
		return err
	}
	return tx.Commit()
}
