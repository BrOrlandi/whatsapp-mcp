package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *sql.DB }

// Event is one Evolution payload as persisted. A single event can carry many
// messages, because a history sync delivers whole conversations at once.
type Event struct {
	ID, Type, InstanceID string
	Payload              []byte
	ReceivedAt           time.Time
	Messages             []Message
}
type Message struct {
	InstanceID string    `json:"instance_id"`
	MessageID  string    `json:"message_id"`
	ChatJID    string    `json:"chat_jid"`
	SenderJID  string    `json:"sender_jid,omitempty"`
	SenderName string    `json:"sender_name,omitempty"`
	FromMe     bool      `json:"from_me"`
	IsGroup    bool      `json:"is_group"`
	MediaType  string    `json:"media_type,omitempty"`
	Text       string    `json:"text"`
	SentAt     time.Time `json:"sent_at"`
}

var ErrAdminExists = errors.New("admin already exists")

// ErrInstanceUnknown reports an instance this panel does not manage, and for
// which it therefore holds no Evolution token.
var ErrInstanceUnknown = errors.New("instance is not managed by this panel")

func (s *Store) Admin(ctx context.Context) (string, string, error) {
	var username, hash string
	err := s.DB.QueryRowContext(ctx, `SELECT username,password_hash FROM control_panel_admin WHERE singleton=TRUE`).Scan(&username, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return username, hash, err
}

func (s *Store) CreateAdmin(ctx context.Context, username, hash string) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO control_panel_admin(singleton,username,password_hash) VALUES(TRUE,$1,$2) ON CONFLICT(singleton) DO NOTHING`, username, hash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrAdminExists
	}
	return nil
}

func (s *Store) SelectedInstance(ctx context.Context) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT selected_instance_id FROM control_panel_settings WHERE singleton=TRUE`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) SelectInstance(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO control_panel_settings(singleton,selected_instance_id) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET selected_instance_id=EXCLUDED.selected_instance_id,updated_at=now()`, id)
	return err
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) Healthy(ctx context.Context) bool {
	return s != nil && s.DB != nil && s.DB.PingContext(ctx) == nil
}

func (s *Store) PersistEvent(ctx context.Context, event Event) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO events (event_id,event_type,instance_id,payload,received_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`, event.ID, event.Type, event.InstanceID, event.Payload, event.ReceivedAt); err != nil {
		return err
	}
	for _, m := range event.Messages {
		if m.MessageID == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO messages (instance_id,message_id,chat_jid,sender_jid,sender_name,from_me,is_group,media_type,text,sent_at,event_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (instance_id,message_id) DO NOTHING`,
			m.InstanceID, m.MessageID, m.ChatJID, m.SenderJID, m.SenderName, m.FromMe, m.IsGroup, m.MediaType, m.Text, nullableTime(m.SentAt), event.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SearchMessages(ctx context.Context, query string, limit int) ([]Message, error) {
	if query == "" {
		return nil, errors.New("query is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.instance_id,m.message_id,m.chat_jid,m.sender_jid,m.sender_name,m.from_me,m.is_group,m.media_type,m.text,m.sent_at FROM messages m JOIN control_panel_settings s ON s.singleton=TRUE AND s.selected_instance_id=m.instance_id WHERE m.search_vector @@ websearch_to_tsquery('simple',$1) ORDER BY m.sent_at DESC NULLS LAST LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Message
	for rows.Next() {
		var m Message
		var sent sql.NullTime
		if err := rows.Scan(&m.InstanceID, &m.MessageID, &m.ChatJID, &m.SenderJID, &m.SenderName, &m.FromMe, &m.IsGroup, &m.MediaType, &m.Text, &sent); err != nil {
			return nil, err
		}
		if sent.Valid {
			m.SentAt = sent.Time
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (s *Store) Close() error {
	if s != nil && s.DB != nil {
		return s.DB.Close()
	}
	return nil
}

// SaveInstance records an instance created through the control panel together
// with the token that authenticates every later per-instance Evolution call.
func (s *Store) SaveInstance(ctx context.Context, id, name, token string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO evolution_instances(instance_id,name,token) VALUES($1,$2,$3) ON CONFLICT(instance_id) DO UPDATE SET name=EXCLUDED.name,token=EXCLUDED.token`, id, name, token)
	return err
}

// InstanceToken returns the stored Evolution token for an instance. An instance
// this panel did not create has no token here and cannot be operated.
func (s *Store) InstanceToken(ctx context.Context, id string) (string, error) {
	var token string
	err := s.DB.QueryRowContext(ctx, `SELECT token FROM evolution_instances WHERE instance_id=$1`, id).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInstanceUnknown
	}
	return token, err
}

// ForgetInstance drops the local record of an instance. It is called after the
// instance is removed from Evolution.
func (s *Store) ForgetInstance(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM evolution_instances WHERE instance_id=$1`, id)
	return err
}

// ManagedInstances lists the instance ids this panel created.
func (s *Store) ManagedInstances(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT instance_id,name FROM evolution_instances`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	managed := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		managed[id] = name
	}
	return managed, rows.Err()
}

// Coverage reports what the message index actually holds for one instance: how
// many messages, and the oldest and newest timestamps. It is what lets a tool
// answer "this conversation is not in the index" instead of "this conversation
// does not exist".
type Coverage struct {
	Messages           int64     `json:"messages"`
	OldestAt, NewestAt time.Time `json:"-"`
}

func (s *Store) Coverage(ctx context.Context, instanceID string) (Coverage, error) {
	var coverage Coverage
	var oldest, newest sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT count(*),min(sent_at),max(sent_at) FROM messages WHERE instance_id=$1`, instanceID).Scan(&coverage.Messages, &oldest, &newest)
	if err != nil {
		return Coverage{}, err
	}
	if oldest.Valid {
		coverage.OldestAt = oldest.Time.UTC()
	}
	if newest.Valid {
		coverage.NewestAt = newest.Time.UTC()
	}
	return coverage, nil
}
