package transcribe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type memoryStore struct {
	key         string
	transcripts map[string]store.Transcript
}

func (m *memoryStore) TranscriptionKey(context.Context) (store.TranscriptionKey, error) {
	if m.key == "" {
		return store.TranscriptionKey{}, store.ErrNoTranscriptionKey
	}
	return store.TranscriptionKey{APIKey: m.key}, nil
}
func (m *memoryStore) SaveTranscriptionKey(_ context.Context, key string) error {
	m.key = key
	return nil
}
func (m *memoryStore) ClearTranscriptionKey(context.Context) error {
	m.key = ""
	return nil
}
func (m *memoryStore) Transcript(_ context.Context, instance, message string) (store.Transcript, error) {
	t, ok := m.transcripts[instance+"/"+message]
	if !ok {
		return store.Transcript{}, store.ErrNoTranscript
	}
	return t, nil
}
func (m *memoryStore) SaveTranscript(_ context.Context, t store.Transcript) error {
	if m.transcripts == nil {
		m.transcripts = map[string]store.Transcript{}
	}
	m.transcripts[t.InstanceID+"/"+t.MessageID] = t
	return nil
}

const goodKey = "sk-proj-abcdefghijklmnopqrstuvwxyz1234"

// fakeOpenAI answers like OpenAI for goodKey and refuses anything else.
func fakeOpenAI(t *testing.T, form map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided","code":"invalid_api_key"}}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models/whisper-1":
			_, _ = io.WriteString(w, `{"id":"whisper-1","object":"model"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			}
			for name := range r.MultipartForm.Value {
				form[name] = r.FormValue(name)
			}
			_, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("no file: %v", err)
			} else {
				form["filename"] = header.Filename
			}
			_, _ = io.WriteString(w, `{"task":"transcribe","language":"portuguese","duration":4.2,"text":" Oi, tudo bem? "}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSaveKeyChecksWithOpenAIBeforeSaving(t *testing.T) {
	api := fakeOpenAI(t, map[string]string{})
	defer api.Close()
	memory := &memoryStore{}
	service := NewWithEndpoint(memory, api.URL, api.Client())

	if _, err := service.SaveKey(context.Background(), "not a key"); !errors.Is(err, ErrMalformedKey) {
		t.Fatalf("malformed key: %v", err)
	}
	if _, err := service.SaveKey(context.Background(), "sk-wrongwrongwrongwrongwrong"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("refused key: %v", err)
	}
	if memory.key != "" {
		t.Fatalf("a refused key was saved: %q", memory.key)
	}
	status, err := service.SaveKey(context.Background(), "  "+goodKey+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if memory.key != goodKey || !status.Configured || status.Hint != "sk-…1234" {
		t.Fatalf("saved %q, status %+v", memory.key, status)
	}
	if strings.Contains(status.Hint, "abcdef") {
		t.Fatalf("hint reveals the key: %q", status.Hint)
	}
	if err := service.RemoveKey(context.Background()); err != nil || memory.key != "" {
		t.Fatalf("remove: %v, key %q", err, memory.key)
	}
}

func TestTranscribeSendsTheAudioAndKeepsTheTranscript(t *testing.T) {
	form := map[string]string{}
	api := fakeOpenAI(t, form)
	defer api.Close()
	memory := &memoryStore{key: goodKey}
	service := NewWithEndpoint(memory, api.URL, api.Client())

	transcript, err := service.Transcribe(context.Background(), "inst-1", "M1", Audio{MimeType: "audio/ogg; codecs=opus", Data: []byte("OggS...")}, "PT")
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Text != "Oi, tudo bem?" || transcript.Language != "portuguese" || transcript.Model != Model || transcript.Duration != 4.2 {
		t.Fatalf("transcript %+v", transcript)
	}
	if form["model"] != "whisper-1" || form["language"] != "pt" || form["filename"] != "audio.ogg" {
		t.Fatalf("form %+v", form)
	}
	kept, err := service.Stored(context.Background(), "inst-1", "M1")
	if err != nil || kept.Text != "Oi, tudo bem?" {
		t.Fatalf("kept %+v, %v", kept, err)
	}
}

func TestTranscribeRefusesWhatWhisperCannotTake(t *testing.T) {
	memory := &memoryStore{}
	service := NewWithEndpoint(memory, "http://127.0.0.1:1", http.DefaultClient)
	if _, err := service.Transcribe(context.Background(), "i", "m", Audio{MimeType: "audio/ogg", Data: []byte("x")}, ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("without a key: %v", err)
	}
	memory.key = goodKey
	if _, err := service.Transcribe(context.Background(), "i", "m", Audio{MimeType: "audio/amr", Data: []byte("x")}, ""); !errors.Is(err, ErrUnsupportedAudio) {
		t.Fatalf("amr: %v", err)
	}
	if _, err := service.Transcribe(context.Background(), "i", "m", Audio{MimeType: "audio/ogg", Data: make([]byte, MaxAudioBytes+1)}, ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}

func TestOpenAIErrorsAreNamed(t *testing.T) {
	if err := openAIError(http.StatusTooManyRequests, []byte(`{"error":{"message":"You exceeded your current quota","code":"insufficient_quota"}}`)); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota: %v", err)
	}
	if err := openAIError(http.StatusBadRequest, []byte(`{"error":{"message":"Invalid file format."}}`)); err == nil || !strings.Contains(err.Error(), "Invalid file format") {
		t.Fatalf("bad request: %v", err)
	}
}
