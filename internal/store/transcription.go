package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNoTranscriptionKey reports that no OpenAI key has been saved, which is
// the ordinary state of a deployment that never asked for transcription.
var ErrNoTranscriptionKey = errors.New("no OpenAI API key is saved for transcription")

// ErrNoTranscript reports that a message has not been transcribed yet.
var ErrNoTranscript = errors.New("message has not been transcribed")

// TranscriptionKey is the saved OpenAI credential and when it last changed.
type TranscriptionKey struct {
	APIKey    string
	UpdatedAt time.Time
}

// Transcript is what Whisper heard in one voice note.
type Transcript struct {
	InstanceID string    `json:"-"`
	MessageID  string    `json:"message_id"`
	Text       string    `json:"text"`
	Language   string    `json:"language,omitempty"`
	Model      string    `json:"model"`
	Duration   float64   `json:"duration_seconds,omitempty"`
	CreatedAt  time.Time `json:"transcribed_at"`
}

// TranscriptionKey returns the saved key, or ErrNoTranscriptionKey.
func (s *Store) TranscriptionKey(ctx context.Context) (TranscriptionKey, error) {
	var key TranscriptionKey
	err := s.DB.QueryRowContext(ctx, `SELECT openai_api_key,updated_at FROM transcription_settings WHERE singleton=TRUE`).Scan(&key.APIKey, &key.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && key.APIKey == "") {
		return TranscriptionKey{}, ErrNoTranscriptionKey
	}
	return key, err
}

// SaveTranscriptionKey replaces the saved key.
func (s *Store) SaveTranscriptionKey(ctx context.Context, apiKey string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO transcription_settings(singleton,openai_api_key) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET openai_api_key=EXCLUDED.openai_api_key, updated_at=now()`, apiKey)
	return err
}

// ClearTranscriptionKey forgets the saved key. Transcripts already made are
// kept: they were paid for, and they stay readable without a key.
func (s *Store) ClearTranscriptionKey(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM transcription_settings WHERE singleton=TRUE`)
	return err
}

// Transcript returns the stored transcript of one message, or ErrNoTranscript.
func (s *Store) Transcript(ctx context.Context, instanceID, messageID string) (Transcript, error) {
	t := Transcript{InstanceID: instanceID, MessageID: messageID}
	err := s.DB.QueryRowContext(ctx, `SELECT text,language,model,duration_seconds,created_at FROM transcriptions WHERE instance_id=$1 AND message_id=$2`, instanceID, messageID).Scan(&t.Text, &t.Language, &t.Model, &t.Duration, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Transcript{}, ErrNoTranscript
	}
	return t, err
}

// SaveTranscript records a transcript. A second one for the same message
// replaces the first, which is what an explicit retry in another language asks
// for.
func (s *Store) SaveTranscript(ctx context.Context, t Transcript) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO transcriptions(instance_id,message_id,text,language,model,duration_seconds) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(instance_id,message_id) DO UPDATE SET text=EXCLUDED.text, language=EXCLUDED.language, model=EXCLUDED.model, duration_seconds=EXCLUDED.duration_seconds, created_at=now()`, t.InstanceID, t.MessageID, t.Text, t.Language, t.Model, t.Duration)
	return err
}
