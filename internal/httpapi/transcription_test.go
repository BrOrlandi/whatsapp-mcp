package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
)

type fakeTranscription struct {
	key string
}

func (f *fakeTranscription) Status(context.Context) (transcribe.Status, error) {
	if f.key == "" {
		return transcribe.Status{}, nil
	}
	return transcribe.Status{Configured: true, Hint: transcribe.Hint(f.key), UpdatedAt: time.Now()}, nil
}
func (f *fakeTranscription) SaveKey(_ context.Context, key string) (transcribe.Status, error) {
	if !strings.HasPrefix(key, "sk-") {
		return transcribe.Status{}, transcribe.ErrMalformedKey
	}
	if strings.HasSuffix(key, "bad") {
		return transcribe.Status{}, transcribe.ErrInvalidKey
	}
	f.key = key
	return f.Status(context.Background())
}
func (f *fakeTranscription) RemoveKey(context.Context) error {
	f.key = ""
	return nil
}

func transcriptionPanel(t *testing.T, transcription TranscriptionSettings) (*httptest.Server, *http.Client) {
	t.Helper()
	repo := newRepo()
	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo.user, repo.hash = "admin", hash
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, transcription))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return ts, client
}

func postPage(t *testing.T, client *http.Client, target string, form url.Values) string {
	t.Helper()
	r, err := client.PostForm(target, form)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	return string(body)
}

// The panel saves the key, shows only its hint afterwards, explains a refusal
// in Portuguese, and can forget it again.
func TestTranscriptionPageSavesShowsTheHintAndRemoves(t *testing.T) {
	const key = "sk-proj-supersecretvalue-4321"
	transcription := &fakeTranscription{}
	ts, client := transcriptionPanel(t, transcription)

	page := fetch(t, client, ts.URL+"/transcricao")
	mustContain(t, page, "transcricao", "Transcrição de áudios", "Não configurada", `name="api_key"`)

	page = postPage(t, client, ts.URL+"/transcricao", url.Values{"api_key": {"sk-refused-bad"}})
	mustContain(t, page, "transcricao", "A OpenAI recusou esta chave")
	if transcription.key != "" {
		t.Fatalf("a refused key was saved: %q", transcription.key)
	}

	page = postPage(t, client, ts.URL+"/transcricao", url.Values{"api_key": {key}})
	mustContain(t, page, "transcricao", "Chave salva", "Configurada", "sk-…4321")
	if strings.Contains(page, "supersecret") {
		t.Fatal("the page shows the saved key")
	}

	page = postPage(t, client, ts.URL+"/transcricao/remover", nil)
	mustContain(t, page, "transcricao", "Chave removida", "Não configurada")
	if transcription.key != "" {
		t.Fatal("the key was not removed")
	}
}

func TestTranscriptionPageNeedsASession(t *testing.T) {
	transcription := &fakeTranscription{}
	ts, _ := transcriptionPanel(t, transcription)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.PostForm(ts.URL+"/transcricao", url.Values{"api_key": {"sk-proj-supersecretvalue-4321"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/login" || transcription.key != "" {
		t.Fatalf("status %d, location %q, key %q", r.StatusCode, r.Header.Get("Location"), transcription.key)
	}
}

func TestTranscriptionPageWithoutTheServiceExplainsIt(t *testing.T) {
	ts, client := transcriptionPanel(t, nil)
	mustContain(t, fetch(t, client, ts.URL+"/transcricao"), "transcricao", "não está disponível")
}
