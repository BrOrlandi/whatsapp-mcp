// Package transcribe turns WhatsApp voice notes into text through OpenAI's
// Whisper, with a key the operator saves from the panel or from an MCP client.
//
// It is one service shared by both façades, like everything else here: the
// panel and the MCP tools save the same key through the same validation, so a
// key refused in one place is refused for the same reason in the other.
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Model is the Whisper model every transcript is made with.
const Model = "whisper-1"

// MaxAudioBytes is the largest file OpenAI's transcription endpoint accepts.
const MaxAudioBytes = 25 << 20

const defaultBaseURL = "https://api.openai.com/v1"

// The OpenAI pages an operator needs on the way to a key and after it. The
// panel links them and the MCP tools name them, so both give the same
// directions.
const (
	SignupURL  = "https://platform.openai.com/signup"
	BillingURL = "https://platform.openai.com/settings/organization/billing/overview"
	KeysURL    = "https://platform.openai.com/api-keys"
	UsageURL   = "https://platform.openai.com/usage"
	LimitsURL  = "https://platform.openai.com/settings/organization/limits"
	PricingURL = "https://openai.com/api/pricing/"
)

// PricePerMinute is Whisper's price in US dollars, quoted to set expectations
// before anyone adds credit.
const PricePerMinute = 0.006

var (
	// ErrNotConfigured means no key was saved, so nothing can be transcribed.
	ErrNotConfigured = errors.New("transcription is not configured: save an OpenAI API key in the control panel or with set_transcription_key")
	// ErrInvalidKey means OpenAI refused the key.
	ErrInvalidKey = errors.New("OpenAI refused this API key")
	// ErrMalformedKey means the text is not shaped like an OpenAI key at all,
	// which is caught before it is sent anywhere.
	ErrMalformedKey = errors.New("this does not look like an OpenAI API key, which starts with sk-")
	// ErrQuota means the OpenAI account has no credit left.
	ErrQuota = errors.New("the OpenAI account behind this key has no quota left; check its billing")
	// ErrUnsupportedAudio means Whisper cannot read this audio format.
	ErrUnsupportedAudio = errors.New("Whisper does not accept this audio format")
	// ErrTooLarge means the audio is past OpenAI's upload limit.
	ErrTooLarge = errors.New("the audio is larger than the 25 MB OpenAI accepts")
)

// Store is where the key and the transcripts are kept.
type Store interface {
	TranscriptionKey(context.Context) (store.TranscriptionKey, error)
	SaveTranscriptionKey(context.Context, string) error
	ClearTranscriptionKey(context.Context) error
	Transcript(context.Context, string, string) (store.Transcript, error)
	SaveTranscript(context.Context, store.Transcript) error
}

// Audio is one decoded voice note.
type Audio struct {
	MimeType string
	Data     []byte
}

// Status is what may be shown about the saved key: whether there is one, and
// enough of it to recognise, never enough to use.
type Status struct {
	Configured bool      `json:"configured"`
	Hint       string    `json:"key_hint,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type Service struct {
	store   Store
	client  *http.Client
	baseURL string
}

// New builds the service against OpenAI.
func New(s Store) *Service {
	return NewWithEndpoint(s, defaultBaseURL, &http.Client{Timeout: 55 * time.Second})
}

// NewWithEndpoint points the service at another OpenAI-compatible endpoint,
// which is what the tests use.
func NewWithEndpoint(s Store, baseURL string, client *http.Client) *Service {
	return &Service{store: s, client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

// Status reports whether a key is saved.
func (s *Service) Status(ctx context.Context) (Status, error) {
	key, err := s.store.TranscriptionKey(ctx)
	if errors.Is(err, store.ErrNoTranscriptionKey) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Configured: true, Hint: Hint(key.APIKey), UpdatedAt: key.UpdatedAt}, nil
}

// SaveKey checks the key with OpenAI and saves it. A key OpenAI refuses is not
// saved: finding that out on the first voice note, from inside an AI client,
// is a worse place to learn it than the form it was typed into.
func (s *Service) SaveKey(ctx context.Context, apiKey string) (Status, error) {
	apiKey = strings.TrimSpace(apiKey)
	if !plausibleKey(apiKey) {
		return Status{}, ErrMalformedKey
	}
	if err := s.checkKey(ctx, apiKey); err != nil {
		return Status{}, err
	}
	if err := s.store.SaveTranscriptionKey(ctx, apiKey); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// RemoveKey forgets the saved key.
func (s *Service) RemoveKey(ctx context.Context) error {
	return s.store.ClearTranscriptionKey(ctx)
}

// Stored returns a transcript made earlier, or store.ErrNoTranscript.
func (s *Service) Stored(ctx context.Context, instanceID, messageID string) (store.Transcript, error) {
	return s.store.Transcript(ctx, instanceID, messageID)
}

// Transcribe sends one voice note to Whisper and keeps the answer.
func (s *Service) Transcribe(ctx context.Context, instanceID, messageID string, audio Audio, language string) (store.Transcript, error) {
	key, err := s.store.TranscriptionKey(ctx)
	if errors.Is(err, store.ErrNoTranscriptionKey) {
		return store.Transcript{}, ErrNotConfigured
	}
	if err != nil {
		return store.Transcript{}, err
	}
	if len(audio.Data) > MaxAudioBytes {
		return store.Transcript{}, ErrTooLarge
	}
	filename, err := audioFilename(audio.MimeType)
	if err != nil {
		return store.Transcript{}, err
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("model", Model)
	_ = form.WriteField("response_format", "verbose_json")
	if language = strings.ToLower(strings.TrimSpace(language)); language != "" {
		_ = form.WriteField("language", language)
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return store.Transcript{}, err
	}
	if _, err := part.Write(audio.Data); err != nil {
		return store.Transcript{}, err
	}
	if err := form.Close(); err != nil {
		return store.Transcript{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/audio/transcriptions", &body)
	if err != nil {
		return store.Transcript{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key.APIKey)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		return store.Transcript{}, fmt.Errorf("could not reach OpenAI: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return store.Transcript{}, openAIError(resp.StatusCode, raw)
	}
	var answer struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return store.Transcript{}, fmt.Errorf("OpenAI answered with something that is not a transcript")
	}
	transcript := store.Transcript{
		InstanceID: instanceID,
		MessageID:  messageID,
		Text:       strings.TrimSpace(answer.Text),
		Language:   answer.Language,
		Model:      Model,
		Duration:   answer.Duration,
		CreatedAt:  time.Now().UTC(),
	}
	// A transcript that could not be kept is still a transcript: the caller
	// asked for the text, and it is in hand. Only the next request pays again.
	_ = s.store.SaveTranscript(ctx, transcript)
	return transcript, nil
}

// checkKey asks OpenAI whether the key can see the Whisper model, which costs
// nothing. A restricted key may be allowed to transcribe and still not to read
// the model list; that is a 403, and the key is accepted, because the first
// transcription is then the only honest test left.
func (s *Service) checkKey(ctx context.Context, apiKey string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/models/"+Model, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach OpenAI to check the key: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode/100 == 2, resp.StatusCode == http.StatusForbidden:
		return nil
	default:
		return openAIError(resp.StatusCode, raw)
	}
}

// openAIError turns OpenAI's error envelope into one of the failures a person
// can act on.
func openAIError(status int, raw []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &envelope)
	switch {
	case status == http.StatusUnauthorized:
		return ErrInvalidKey
	case envelope.Error.Code == "insufficient_quota":
		return ErrQuota
	case envelope.Error.Message != "":
		return fmt.Errorf("OpenAI answered %d: %s", status, envelope.Error.Message)
	default:
		return fmt.Errorf("OpenAI answered %d", status)
	}
}

// audioFilename names the upload after its format, because OpenAI decides how
// to decode the file from its extension rather than from a content type.
func audioFilename(mimeType string) (string, error) {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	}
	switch base {
	case "audio/ogg", "audio/opus", "application/ogg":
		return "audio.ogg", nil
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3", nil
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac", "video/mp4":
		return "audio.m4a", nil
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio.wav", nil
	case "audio/webm", "video/webm":
		return "audio.webm", nil
	case "audio/flac", "audio/x-flac":
		return "audio.flac", nil
	}
	return "", fmt.Errorf("%w (%s)", ErrUnsupportedAudio, mimeType)
}

// plausibleKey rejects what is obviously not a key — a sentence, an empty
// field, something with spaces — before it is sent to OpenAI.
func plausibleKey(key string) bool {
	if !strings.HasPrefix(key, "sk-") || len(key) < 20 || len(key) > 400 {
		return false
	}
	for _, r := range key {
		if unicode.IsSpace(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// Hint is the part of a key that is safe to show: enough to tell two keys
// apart, never enough to use one.
func Hint(key string) string {
	if len(key) < 8 {
		return "sk-…"
	}
	return "sk-…" + key[len(key)-4:]
}
