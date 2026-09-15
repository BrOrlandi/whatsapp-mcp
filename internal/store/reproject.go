package store

import (
	"context"
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
		result, err := tx.ExecContext(ctx, `INSERT INTO messages (instance_id,message_id,chat_jid,sender_jid,sender_name,from_me,is_group,media_type,text,sent_at,event_id)
                        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (instance_id,message_id) DO NOTHING`,
			m.InstanceID, m.MessageID, m.ChatJID, m.SenderJID, m.SenderName, m.FromMe, m.IsGroup, m.MediaType, m.Text, nullableTime(m.SentAt), eventID)
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
