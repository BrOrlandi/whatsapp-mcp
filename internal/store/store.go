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

type Event struct {
	ID, Type, InstanceID string
	Payload              []byte
	ReceivedAt           time.Time
	Message              *Message
}
type Message struct {
	InstanceID string    `json:"instance_id"`
	MessageID  string    `json:"message_id"`
	ChatJID    string    `json:"chat_jid"`
	SenderName string    `json:"sender_name,omitempty"`
	FromMe     bool      `json:"from_me"`
	Text       string    `json:"text"`
	SentAt     time.Time `json:"sent_at"`
}

var ErrAdminExists = errors.New("admin already exists")

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
	if event.Message != nil {
		m := event.Message
		if _, err = tx.ExecContext(ctx, `INSERT INTO messages (instance_id,message_id,chat_jid,sender_name,from_me,text,sent_at,event_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (instance_id,message_id) DO NOTHING`, m.InstanceID, m.MessageID, m.ChatJID, m.SenderName, m.FromMe, m.Text, nullableTime(m.SentAt), event.ID); err != nil {
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
	rows, err := s.DB.QueryContext(ctx, `SELECT m.instance_id,m.message_id,m.chat_jid,m.sender_name,m.from_me,m.text,m.sent_at FROM messages m JOIN control_panel_settings s ON s.singleton=TRUE AND s.selected_instance_id=m.instance_id WHERE m.search_vector @@ websearch_to_tsquery('simple',$1) ORDER BY m.sent_at DESC NULLS LAST LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Message
	for rows.Next() {
		var m Message
		var sent sql.NullTime
		if err := rows.Scan(&m.InstanceID, &m.MessageID, &m.ChatJID, &m.SenderName, &m.FromMe, &m.Text, &sent); err != nil {
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
