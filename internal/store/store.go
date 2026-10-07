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
	// Aliases pair a chat's LID with its phone-number JID, as the event
	// revealed them.
	Aliases []Alias
	// Edits change messages already indexed: a new text, or a deletion.
	Edits []Edit
	// Chats are what a history sync says about each conversation.
	Chats []ChatState
	// Reads are the moments up to which the account read a chat.
	Reads []ChatRead
	// Skip keeps the event itself out of the events table: its effect is
	// applied, but the payload is not worth keeping (receipts).
	Skip bool
}

// Edit changes an indexed message: a new text, or its deletion for everyone.
//
// SenderJID and FromMe say who sent the edit: only the author of a message
// can edit it, and only its author (or, in a group, an admin) can delete it
// for everyone, so an edit from anyone else is ignored rather than trusted.
type Edit struct {
	InstanceID string
	ChatJID    string
	MessageID  string
	Text       string
	Revoked    bool
	SenderJID  string
	FromMe     bool
}

// ChatState is what a history sync says the phone shows about a
// conversation. HasUnread says UnreadCount is known, as of SnapshotAt.
//
// The flags are pointers: a sync that leaves one out says nothing about it,
// and must not reset what an earlier one said.
type ChatState struct {
	InstanceID   string
	ChatJID      string
	HasUnread    bool
	UnreadCount  int
	SnapshotAt   time.Time
	MarkedUnread *bool
	Archived     *bool
	Pinned       *bool
	MutedUntil   *time.Time
	Name         string
}

// ChatRead says the account read a chat up to a moment.
type ChatRead struct {
	InstanceID string
	ChatJID    string
	At         time.Time
}

// Alias says that a LID chat and a phone-number chat are the same
// conversation.
type Alias struct {
	InstanceID string
	LID        string
	PN         string
}
type Message struct {
	InstanceID string `json:"instance_id"`
	MessageID  string `json:"message_id"`
	ChatJID    string `json:"chat_jid"`
	SenderJID  string `json:"sender_jid,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	FromMe     bool   `json:"from_me"`
	IsGroup    bool   `json:"is_group"`
	MediaType  string `json:"media_type,omitempty"`
	Text       string `json:"text"`
	// Transcript is what was said in a voice note, when it has been
	// transcribed. Only the reading queries fill it in.
	Transcript string    `json:"transcript,omitempty"`
	SentAt     time.Time `json:"sent_at"`
	MimeType   string    `json:"mime_type,omitempty"`
	Filename   string    `json:"filename,omitempty"`
	// QuotedID is the message this one replies to.
	QuotedID string `json:"quoted_message_id,omitempty"`
	// Mentions are the JIDs the message mentions.
	Mentions   []string `json:"mentions,omitempty"`
	Forwarded  bool     `json:"forwarded,omitempty"`
	ReactionTo string   `json:"reaction_to,omitempty"`
	Reaction   string   `json:"reaction,omitempty"`
	Edited     bool     `json:"edited,omitempty"`
	Revoked    bool     `json:"revoked,omitempty"`
	// ChatName is the conversation's name; only the reading tools fill it.
	ChatName string `json:"chat_name,omitempty"`
	// TextTruncated says max_content_chars cut the text or the transcript.
	TextTruncated bool `json:"text_truncated,omitempty"`
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
	return s.createAdmin(ctx, username, hash, false)
}

func (s *Store) createAdmin(ctx context.Context, username, hash string, mustChange bool) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO control_panel_admin(singleton,username,password_hash,must_change_password) VALUES(TRUE,$1,$2,$3) ON CONFLICT(singleton) DO NOTHING`, username, hash, mustChange)
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

// AdminMustChangePassword reports whether the panel should refuse to do
// anything else until the password is replaced.
func (s *Store) AdminMustChangePassword(ctx context.Context) (bool, error) {
	var must bool
	err := s.DB.QueryRowContext(ctx, `SELECT must_change_password FROM control_panel_admin WHERE singleton=TRUE`).Scan(&must)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return must, err
}

// SetAdminPassword replaces the password and clears the rotation flag, which
// are the same event and must not be able to happen separately.
func (s *Store) SetAdminPassword(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE control_panel_admin SET password_hash=$1, must_change_password=FALSE WHERE singleton=TRUE`, hash)
	return err
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
	if !event.Skip {
		if _, err = tx.ExecContext(ctx, `INSERT INTO events (event_id,event_type,instance_id,payload,received_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`, event.ID, event.Type, event.InstanceID, event.Payload, event.ReceivedAt); err != nil {
			return err
		}
	}
	for _, m := range event.Messages {
		if m.MessageID == "" || event.Skip {
			continue
		}
		if _, err = insertMessage(ctx, tx, m, event.ID); err != nil {
			return err
		}
	}
	for _, a := range event.Aliases {
		if a.InstanceID == "" || a.LID == "" || a.PN == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO jid_aliases (instance_id,lid,pn) VALUES ($1,$2,$3) ON CONFLICT (instance_id,lid) DO UPDATE SET pn=EXCLUDED.pn, updated_at=now() WHERE jid_aliases.pn <> EXCLUDED.pn`, a.InstanceID, a.LID, a.PN); err != nil {
			return err
		}
	}
	if err = applyEdits(ctx, tx, event.Edits); err != nil {
		return err
	}
	if err = applyChatStates(ctx, tx, event.Chats); err != nil {
		return err
	}
	if err = applyReads(ctx, tx, event.Reads); err != nil {
		return err
	}
	return tx.Commit()
}

// DetailsVersion is the decoder version whose details a message row holds.
// It matches events.DecoderVersion; the store cannot import that package.
const DetailsVersion = 3

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// applyEdits changes the messages an edit or a deletion points at, in the
// same chat, when it came from the message's author. A deletion in a group
// may also come from an admin. A target not indexed yet is left alone: there
// is nothing to change.
func applyEdits(ctx context.Context, tx execer, edits []Edit) error {
	const bare = `regexp_replace(%s, ':[0-9]+@', '@')`
	author := `(m.from_me = $4 AND (m.from_me OR ` + fmt.Sprintf(bare, "m.sender_jid") + ` = ANY(ARRAY(
			SELECT ` + fmt.Sprintf(bare, "$5::text") + `
			UNION SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = ` + fmt.Sprintf(bare, "$5::text") + `
			UNION SELECT lid FROM jid_aliases WHERE instance_id = $1 AND pn = ` + fmt.Sprintf(bare, "$5::text") + `))))`
	sameChat := `m.chat_jid = ANY(ARRAY(SELECT $3::text UNION SELECT lid FROM jid_aliases WHERE instance_id = $1 AND pn = $3 UNION SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = $3))`
	for _, e := range edits {
		if e.InstanceID == "" || e.MessageID == "" {
			continue
		}
		var err error
		if e.Revoked {
			_, err = tx.ExecContext(ctx, `UPDATE messages m SET revoked = TRUE WHERE m.instance_id = $1 AND m.message_id = $2 AND `+sameChat+`
				AND (`+author+` OR m.is_group)`, e.InstanceID, e.MessageID, e.ChatJID, e.FromMe, e.SenderJID)
		} else if e.Text != "" {
			_, err = tx.ExecContext(ctx, `UPDATE messages m SET text = $6, edited = TRUE WHERE m.instance_id = $1 AND m.message_id = $2 AND `+sameChat+`
				AND `+author, e.InstanceID, e.MessageID, e.ChatJID, e.FromMe, e.SenderJID, e.Text)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// canonicalChat is the SQL that maps a chat to the JID it is listed under:
// its phone number when it is a LID paired with one.
const canonicalChat = `coalesce((SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = $2), $2)`

// applyChatStates records what a history sync says about each chat. A sync
// is a snapshot of an earlier moment, so it never moves read_until back, and
// a flag it leaves out keeps the value it had.
func applyChatStates(ctx context.Context, tx execer, chats []ChatState) error {
	for _, c := range chats {
		if c.InstanceID == "" || c.ChatJID == "" {
			continue
		}
		// The unread count becomes the moment the account read up to: the
		// snapshot itself when nothing was unread, or just before the n-th
		// newest message from someone else the index holds up to it. When
		// the index holds fewer than n, every one it holds is unread.
		incoming := `FROM messages m
				 WHERE m.instance_id = $1 AND NOT m.from_me AND m.reaction_to = '' AND NOT m.revoked
				   AND m.chat_jid = ANY(ARRAY(SELECT $2::text UNION SELECT lid FROM jid_aliases WHERE instance_id = $1 AND pn = $2 UNION SELECT pn FROM jid_aliases WHERE instance_id = $1 AND lid = $2))
				   AND ($3::timestamptz IS NULL OR m.sent_at <= $3::timestamptz)`
		readUntil := `CASE WHEN NOT $9 THEN NULL WHEN $10 = 0 THEN $3::timestamptz ELSE coalesce(
				(SELECT m.sent_at - interval '1 millisecond' ` + incoming + ` ORDER BY m.sent_at DESC OFFSET $10 - 1 LIMIT 1),
				(SELECT min(m.sent_at) - interval '1 millisecond' ` + incoming + `)) END`
		_, err := tx.ExecContext(ctx, `INSERT INTO chat_state (instance_id, chat_jid, read_until, marked_unread, archived, pinned, muted_until, name, flags_at)
			VALUES ($1, `+canonicalChat+`, `+readUntil+`, coalesce($4, FALSE), coalesce($5, FALSE), coalesce($6, FALSE), $7, $8, coalesce($3::timestamptz, now()))
			ON CONFLICT (instance_id, chat_jid) DO UPDATE SET
				read_until = CASE WHEN EXCLUDED.read_until IS NULL THEN chat_state.read_until
					WHEN chat_state.read_until IS NULL OR EXCLUDED.read_until > chat_state.read_until THEN EXCLUDED.read_until
					ELSE chat_state.read_until END,
				marked_unread = CASE WHEN $4::boolean IS NULL THEN chat_state.marked_unread ELSE $4 END,
				archived = CASE WHEN $5::boolean IS NULL THEN chat_state.archived ELSE $5 END,
				pinned = CASE WHEN $6::boolean IS NULL THEN chat_state.pinned ELSE $6 END,
				muted_until = CASE WHEN $11 THEN $7 ELSE chat_state.muted_until END,
				name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE chat_state.name END,
				flags_at = CASE WHEN $5::boolean IS NULL AND $6::boolean IS NULL AND NOT $11 THEN chat_state.flags_at
					ELSE GREATEST(coalesce(chat_state.flags_at, EXCLUDED.flags_at), EXCLUDED.flags_at) END,
				updated_at = now()`,
			c.InstanceID, c.ChatJID, nullableTime(c.SnapshotAt), c.MarkedUnread, c.Archived, c.Pinned, mutedValue(c.MutedUntil), c.Name,
			c.HasUnread, c.UnreadCount, c.MutedUntil != nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func mutedValue(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return *t
}

// applyReads moves a chat's read_until forward, and clears a hand-made
// "unread" mark, since reading the chat is what clears it on the phone.
func applyReads(ctx context.Context, tx execer, reads []ChatRead) error {
	for _, r := range reads {
		if r.InstanceID == "" || r.ChatJID == "" || r.At.IsZero() {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO chat_state (instance_id, chat_jid, read_until) VALUES ($1, `+canonicalChat+`, $3)
			ON CONFLICT (instance_id, chat_jid) DO UPDATE SET
				read_until = GREATEST(coalesce(chat_state.read_until, EXCLUDED.read_until), EXCLUDED.read_until),
				marked_unread = FALSE, updated_at = now()`, r.InstanceID, r.ChatJID, r.At)
		if err != nil {
			return err
		}
	}
	return nil
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
