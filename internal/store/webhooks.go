package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Webhook is an address that receives a POST for every new message, so a
// script of the operator's can act on it.
type Webhook struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Secret string `json:"-"`
	// Events is what it receives: message, reaction, receipt.
	Events []string `json:"events"`
	// IncludeOwn also delivers the messages the account itself sends.
	IncludeOwn bool `json:"include_own"`
	// Chats limits it to these conversations; empty means all of them.
	Chats     []string  `json:"chats,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`

	DisabledAt     *time.Time `json:"disabled_at,omitempty"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastResult     string     `json:"last_result,omitempty"`
	Delivered      int64      `json:"delivered"`
}

// ErrNoWebhook means no webhook has that id.
var ErrNoWebhook = errors.New("no webhook has that id")

const webhookColumns = `id, url, secret, events, include_own, chats, enabled, created_at, disabled_at, disabled_reason,
	last_attempt_at, last_success_at, last_result, delivered`

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

func scanWebhook(row interface{ Scan(...any) error }) (Webhook, error) {
	var w Webhook
	var events, chats string
	var disabled, attempt, success sql.NullTime
	err := row.Scan(&w.ID, &w.URL, &w.Secret, &events, &w.IncludeOwn, &chats, &w.Enabled, &w.CreatedAt, &disabled, &w.DisabledReason,
		&attempt, &success, &w.LastResult, &w.Delivered)
	w.Events, w.Chats = splitList(events), splitList(chats)
	w.CreatedAt = w.CreatedAt.UTC()
	w.DisabledAt, w.LastAttemptAt, w.LastSuccessAt = timePtr(disabled), timePtr(attempt), timePtr(success)
	return w, err
}

// Webhooks lists every webhook, oldest first.
func (s *Store) Webhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhooks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Webhook returns one webhook, or ErrNoWebhook.
func (s *Store) Webhook(ctx context.Context, id string) (Webhook, error) {
	w, err := scanWebhook(s.DB.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNoWebhook
	}
	return w, err
}

// SaveWebhook creates a webhook or replaces its settings, keeping its
// delivery record.
func (s *Store) SaveWebhook(ctx context.Context, w Webhook) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO webhooks (id, url, secret, events, include_own, chats, enabled, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET url = EXCLUDED.url, events = EXCLUDED.events, include_own = EXCLUDED.include_own,
			chats = EXCLUDED.chats, enabled = EXCLUDED.enabled`,
		w.ID, w.URL, w.Secret, strings.Join(w.Events, ","), w.IncludeOwn, strings.Join(w.Chats, ","), w.Enabled, w.CreatedAt)
	return err
}

// DeleteWebhook removes a webhook, or reports ErrNoWebhook.
func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoWebhook
	}
	return nil
}

// RecordDelivery notes one attempt and its result.
func (s *Store) RecordDelivery(ctx context.Context, id string, at time.Time, result string, ok bool) error {
	query := `UPDATE webhooks SET last_attempt_at = $1, last_result = $2 WHERE id = $3`
	if ok {
		query = `UPDATE webhooks SET last_attempt_at = $1, last_result = $2, last_success_at = $1, delivered = delivered + 1 WHERE id = $3`
	}
	_, err := s.DB.ExecContext(ctx, query, at, result, id)
	return err
}

// DisableWebhook turns a webhook off, saying why.
func (s *Store) DisableWebhook(ctx context.Context, id string, at time.Time, reason string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET enabled = FALSE, disabled_at = $1, disabled_reason = $2 WHERE id = $3`, at, reason, id)
	return err
}

// EnableWebhook turns a webhook back on, forgetting why it was off.
func (s *Store) EnableWebhook(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET enabled = TRUE, disabled_at = NULL, disabled_reason = '' WHERE id = $1`, id)
	return err
}

// SameChat reports whether two chat JIDs are one conversation: equal, or a
// LID and the phone number it is paired with.
func (s *Store) SameChat(ctx context.Context, instanceID, a, b string) bool {
	if a == b {
		return true
	}
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM jid_aliases WHERE instance_id = $1 AND ((lid = $2 AND pn = $3) OR (lid = $3 AND pn = $2))`, instanceID, a, b).Scan(&n)
	return err == nil && n > 0
}
