package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/webhook"
)

// renamingRepo is a store that can also rename a connection.
type renamingRepo struct{ *fakeRepo }

func (r renamingRepo) RenameAPIKey(_ context.Context, id int64, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.keys {
		if r.keys[i].ID == id {
			r.keys[i].Name = name
		}
	}
	return nil
}

type fakeMediaFiles struct {
	mu        sync.Mutex
	retention int
	purged    string
}

func (f *fakeMediaFiles) Inventory(context.Context, string) (mcp.MediaInventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mcp.MediaInventory{Dir: "/data/media", ExportsDir: "/data/exports", Files: 3, Bytes: 3 << 20, RetentionDays: f.retention,
		ByType: map[string]mcp.MediaSum{"audio": {Files: 3, Bytes: 3 << 20}}, ByChat: []mcp.ChatMediaSum{}}, nil
}
func (f *fakeMediaFiles) SetRetention(_ context.Context, days int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retention = days
	return nil
}
func (f *fakeMediaFiles) SweepMedia(context.Context) (int, int64, error) { return 1, 1 << 20, nil }
func (f *fakeMediaFiles) PurgeMedia(string, string, int, int64, bool) (int, int64, []map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = "media"
	return 3, 3 << 20, nil, nil
}
func (f *fakeMediaFiles) PurgeExports() (int, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = "exports"
	return 0, 0, nil
}

func settingsPanel(t *testing.T, store ControlStore, options ...Option) (*httptest.Server, *http.Client) {
	t.Helper()
	ts := httptest.NewServer(NewWebHandler(store, &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}},
		health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, &fakeTranscription{}, options...))
	t.Cleanup(ts.Close)
	client := &http.Client{Jar: newJar(t)}
	r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return ts, client
}

func signedInRepo(t *testing.T) *fakeRepo {
	t.Helper()
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo.user, repo.hash = "admin", hash
	return repo
}

// callJSON sends a JSON request the way settings.js does.
func callJSON(t *testing.T, client *http.Client, method, target string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}

// Configurações is one page with every section of the Local version that a
// server has, in the same order.
func TestSettingsPageCarriesEverySection(t *testing.T) {
	ts, client := settingsPanel(t, signedInRepo(t), WithMediaFiles(&fakeMediaFiles{}))
	page := fetch(t, client, ts.URL+"/configuracoes")
	sections := []string{`id="endereco"`, `id="conta"`, `id="webhooks"`, `id="transcricao"`, `id="atualizacoes"`, `id="aparencia"`, `id="arquivos"`, `id="dados"`}
	last := -1
	for _, section := range sections {
		at := strings.Index(page, section)
		if at < 0 || at < last {
			t.Fatalf("section %s missing or out of order", section)
		}
		last = at
	}
	mustContain(t, page, "settings", "https://mcp.example/mcp", "admin", `src="/assets/settings.js"`, "data-theme-choice", "/data/media", "/webhooks/documentacao")
}

// The webhooks are configured through JSON routes behind the panel's session,
// and a change is accepted only from the panel's own pages.
func TestWebhooksAPIFollowsTheSession(t *testing.T) {
	hooks := webhook.New(webhook.NewMemory(), nil, "test", nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := hooks.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ts, client := settingsPanel(t, signedInRepo(t), WithWebhooks(hooks))

	status, out := callJSON(t, client, http.MethodGet, ts.URL+"/api/webhooks", nil, nil)
	if status != http.StatusOK || out["available"] != true {
		t.Fatalf("list: %d %v", status, out)
	}
	status, out = callJSON(t, client, http.MethodPost, ts.URL+"/api/webhooks", map[string]any{"url": "https://hooks.example/wa", "events": []string{"message", "reaction"}}, nil)
	if status != http.StatusOK || out["secret"] == "" {
		t.Fatalf("create: %d %v", status, out)
	}
	created := out["webhook"].(map[string]any)
	id := created["id"].(string)
	if created["url"] != "https://hooks.example/wa" || created["secret"] != nil {
		t.Fatalf("created webhook = %v", created)
	}
	status, out = callJSON(t, client, http.MethodPost, ts.URL+"/api/webhooks", map[string]any{"url": "ftp://nope"}, nil)
	if status != http.StatusConflict || !strings.Contains(out["error"].(string), "http") {
		t.Fatalf("a bad url: %d %v", status, out)
	}
	status, out = callJSON(t, client, http.MethodPatch, ts.URL+"/api/webhooks/"+id, map[string]any{"enabled": false}, nil)
	if status != http.StatusOK || out["enabled"] != false {
		t.Fatalf("disable: %d %v", status, out)
	}
	// Another site cannot change anything, even with the session cookie.
	status, _ = callJSON(t, client, http.MethodDelete, ts.URL+"/api/webhooks/"+id, map[string]any{}, map[string]string{"Origin": "https://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin delete answered %d", status)
	}
	status, _ = callJSON(t, client, http.MethodDelete, ts.URL+"/api/webhooks/"+id, map[string]any{}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if status != http.StatusForbidden {
		t.Fatalf("cross-site delete answered %d", status)
	}
	// A form post, which a page elsewhere could make, is not JSON.
	r, err := client.PostForm(ts.URL+"/api/webhooks", url.Values{"url": {"https://x.example"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("a form post answered %d", r.StatusCode)
	}
	status, out = callJSON(t, client, http.MethodDelete, ts.URL+"/api/webhooks/"+id, map[string]any{}, nil)
	if status != http.StatusOK || out["deleted"] != true {
		t.Fatalf("delete: %d %v", status, out)
	}
	status, _ = callJSON(t, client, http.MethodGet, ts.URL+"/api/webhooks/"+id, nil, nil)
	if status != http.StatusConflict {
		t.Fatalf("a deleted webhook answered %d", status)
	}
	// Without the session there is nothing to read.
	anonymous, err := http.Get(ts.URL + "/api/webhooks")
	if err != nil {
		t.Fatal(err)
	}
	anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list answered %d", anonymous.StatusCode)
	}
}

// Without webhooks wired in, the card says so instead of offering a form
// that would fail.
func TestWebhooksWithoutAManagerAreUnavailable(t *testing.T) {
	ts, client := settingsPanel(t, signedInRepo(t))
	status, out := callJSON(t, client, http.MethodGet, ts.URL+"/api/webhooks", nil, nil)
	if status != http.StatusOK || out["available"] != false {
		t.Fatalf("list: %d %v", status, out)
	}
	status, _ = callJSON(t, client, http.MethodPost, ts.URL+"/api/webhooks", map[string]any{"url": "https://hooks.example/wa"}, nil)
	if status != http.StatusConflict {
		t.Fatalf("create without a manager answered %d", status)
	}
}

func TestWebhookDocumentationShowsEveryKindOfDelivery(t *testing.T) {
	ts, client := settingsPanel(t, signedInRepo(t))
	page := fetch(t, client, ts.URL+"/webhooks/documentacao")
	mustContain(t, page, "webhook docs", "X-WhatsApp-MCP-Signature", "instance_id", `id="exemplo-texto"`, `id="exemplo-lida"`, `&#34;event&#34;: &#34;reaction&#34;`, "hmac.compare_digest")
}

func TestMediaAPIMeasuresAndClears(t *testing.T) {
	media := &fakeMediaFiles{}
	ts, client := settingsPanel(t, signedInRepo(t), WithMediaFiles(media))
	status, out := callJSON(t, client, http.MethodGet, ts.URL+"/api/media", nil, nil)
	if status != http.StatusOK || out["files"] != float64(3) || out["dir"] != "/data/media" {
		t.Fatalf("inventory: %d %v", status, out)
	}
	status, out = callJSON(t, client, http.MethodPost, ts.URL+"/api/media/retention", map[string]any{"days": 15}, nil)
	if status != http.StatusOK || out["retention_days"] != float64(15) || media.retention != 15 {
		t.Fatalf("retention: %d %v", status, out)
	}
	status, _ = callJSON(t, client, http.MethodPost, ts.URL+"/api/media/purge", map[string]any{"what": "exports"}, nil)
	if status != http.StatusOK || media.purged != "exports" {
		t.Fatalf("purge exports: %d %q", status, media.purged)
	}
	status, _ = callJSON(t, client, http.MethodPost, ts.URL+"/api/media/purge", map[string]any{"what": "everything"}, nil)
	if status != http.StatusConflict {
		t.Fatalf("an unknown purge answered %d", status)
	}
}

// A tool's page offers to create its key, then shows the steps with it, and
// comes back as the "connected" page once that key is used.
func TestAToolsStepsEndWhenItConnects(t *testing.T) {
	repo := signedInRepo(t)
	ts, client := settingsPanel(t, repo)

	page := fetch(t, client, ts.URL+"/conectar/cursor")
	mustContain(t, page, "before", "Conectar o Cursor", "Crie a chave de acesso", `name="cliente" value="cursor"`)
	mustNotContain(t, page, "before", "Bearer wamcp-", "data-wait-key")

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"cursor"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "steps", "~/.cursor/mcp.json", "Bearer wamcp-", `data-wait-key="1"`, `data-wait-tool="cursor"`)
	if len(repo.keys) != 1 || repo.keys[0].Name != "Cursor" {
		t.Fatalf("keys = %+v", repo.keys)
	}

	progress := func() map[string]any {
		_, out := callJSON(t, client, http.MethodGet, ts.URL+"/api/progresso?id=1", nil, nil)
		return out
	}
	if out := progress(); out["used"] != false || len(out) != 1 {
		t.Fatalf("before use = %v", out)
	}
	repo.mu.Lock()
	repo.keys[0].LastUsedAt, repo.keys[0].ClientName = time.Now(), "cursor-vscode"
	repo.mu.Unlock()
	if out := progress(); out["used"] != true {
		t.Fatalf("after use = %v", out)
	}
	done := fetch(t, client, ts.URL+"/conectar/cursor?conectado=1")
	mustContain(t, done, "done", "Cursor conectado", "Voltar ao painel")

	// The tool list and the landing page show it with its own logo.
	mustContain(t, fetch(t, client, ts.URL+"/conectar"), "choose", "Conectado")
	mustContain(t, fetch(t, client, ts.URL+"/"), "landing", "tool-mark--logo", "Cursor")
}

// A connection of a tool the panel has no logo for can be renamed, when the
// store knows how.
func TestAnUnknownToolCanBeRenamed(t *testing.T) {
	repo := signedInRepo(t)
	ts, client := settingsPanel(t, renamingRepo{repo})
	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"outra"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	repo.mu.Lock()
	repo.keys[0].LastUsedAt, repo.keys[0].ClientName = time.Now(), "windsurf"
	repo.mu.Unlock()
	page := fetch(t, client, ts.URL+"/")
	mustContain(t, page, "landing", "Windsurf", `href="#renomear-0"`, `action="/conexoes/renomear"`)
	r, err = client.PostForm(ts.URL+"/conexoes/renomear", url.Values{"id": {"1"}, "name": {"Windsurf do trabalho"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	mustContain(t, fetch(t, client, ts.URL+"/"), "renamed", "Windsurf do trabalho")

	// A store that cannot rename never offers it.
	plain, plainClient := settingsPanel(t, repo)
	mustNotContain(t, fetch(t, plainClient, plain.URL+"/"), "plain", `href="#renomear-0"`)
}

func TestHelpAnswersForTheServer(t *testing.T) {
	ts, client := settingsPanel(t, signedInRepo(t))
	page := fetch(t, client, ts.URL+"/ajuda")
	mustContain(t, page, "help", "Perguntas frequentes", "Como usar", "Transcrever áudios", "Quem está esperando minha resposta no WhatsApp?", "servidor")
	mustNotContain(t, page, "help", "neste computador")
}

// Until an installer-generated password is replaced, the JSON API refuses
// everything, as the pages do: a webhook could otherwise stream every message
// to whoever read the password in a terminal.
func TestAPIWaitsForTheNewPassword(t *testing.T) {
	hooks := webhook.New(webhook.NewMemory(), nil, "test", nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = hooks.Start(ctx)
	repo := signedInRepo(t)
	repo.mustChange = true
	ts, client := settingsPanel(t, repo, WithWebhooks(hooks), WithMediaFiles(&fakeMediaFiles{}))
	for _, call := range []struct{ method, path string }{{http.MethodGet, "/api/webhooks"}, {http.MethodPost, "/api/webhooks"}, {http.MethodPost, "/api/media/purge"}} {
		status, _ := callJSON(t, client, call.method, ts.URL+call.path, map[string]any{"url": "https://hooks.example/wa", "what": "media"}, nil)
		if status != http.StatusForbidden {
			t.Fatalf("%s %s answered %d before the password was changed", call.method, call.path, status)
		}
	}
}

// Behind a proxy that rewrites Host, the browser's Origin is PUBLIC_URL's
// host, and that is the panel's own.
func TestAPIAcceptsThePublicOrigin(t *testing.T) {
	hooks := webhook.New(webhook.NewMemory(), nil, "test", nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = hooks.Start(ctx)
	ts, client := settingsPanel(t, signedInRepo(t), WithWebhooks(hooks))
	status, out := callJSON(t, client, http.MethodPost, ts.URL+"/api/webhooks", map[string]any{"url": "https://hooks.example/wa"},
		map[string]string{"Origin": "https://mcp.example"})
	if status != http.StatusOK {
		t.Fatalf("own public origin refused: %d %v", status, out)
	}
	status, _ = callJSON(t, client, http.MethodPost, ts.URL+"/api/webhooks", map[string]any{"url": "https://hooks.example/wa"},
		map[string]string{"Origin": "https://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("a foreign origin answered %d", status)
	}
}
