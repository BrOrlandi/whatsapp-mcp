package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeRepo struct {
	mu                   sync.Mutex
	user, hash, selected string
	tokens               map[string]string
	names                map[string]string
	coverage             store.Coverage
	keys                 []store.APIKey
	digests              []string
	nextID               int64
	oldest               store.Message
	oldestErr            error
}

func newRepo() *fakeRepo {
	return &fakeRepo{tokens: map[string]string{}, names: map[string]string{}}
}

func (f *fakeRepo) Admin(context.Context) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user == "" {
		return "", "", ErrNotFound
	}
	return f.user, f.hash, nil
}
func (f *fakeRepo) CreateAdmin(_ context.Context, u, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user != "" {
		return ErrAdminExists
	}
	f.user, f.hash = u, h
	return nil
}
func (f *fakeRepo) SelectedInstance(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.selected, nil
}
func (f *fakeRepo) SelectInstance(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.selected = id
	return nil
}
func (f *fakeRepo) SaveInstance(_ context.Context, id, name, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokens == nil {
		f.tokens, f.names = map[string]string{}, map[string]string{}
	}
	f.tokens[id], f.names[id] = token, name
	return nil
}
func (f *fakeRepo) InstanceToken(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := f.tokens[id]
	if !ok {
		return "", ErrNotFound
	}
	return token, nil
}
func (f *fakeRepo) ForgetInstance(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, id)
	delete(f.names, id)
	return nil
}
func (f *fakeRepo) Coverage(_ context.Context, id string) (store.Coverage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.coverage, nil
}
func (f *fakeRepo) CreateAPIKey(_ context.Context, name, instanceID, digest, prefix string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.keys = append(f.keys, store.APIKey{ID: f.nextID, Name: name, InstanceID: instanceID, Prefix: prefix, CreatedAt: time.Now()})
	f.digests = append(f.digests, digest)
	return nil
}
func (f *fakeRepo) ListAPIKeys(context.Context) ([]store.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.APIKey(nil), f.keys...), nil
}
func (f *fakeRepo) RevokeAPIKey(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.keys[:0]
	for _, key := range f.keys {
		if key.ID != id {
			kept = append(kept, key)
		}
	}
	f.keys = kept
	return nil
}
func (f *fakeRepo) OldestMessage(context.Context, string, string) (store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.oldest, f.oldestErr
}
func (f *fakeRepo) ManagedInstances(context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := map[string]string{}
	for id, name := range f.names {
		copied[id] = name
	}
	return copied, nil
}

type fakeEvolution struct {
	mu         sync.Mutex
	instances  []evolution.Instance
	err        error
	qr         evolution.QRCode
	qrErr      error
	createErr  error
	calls      []string
	tokens     []string
	anchors    []evolution.Anchor
	historyErr error
}

func (f *fakeEvolution) record(call, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	f.tokens = append(f.tokens, token)
}
func (f *fakeEvolution) did(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}
func (f *fakeEvolution) FetchInstances(context.Context) ([]evolution.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instances, f.err
}
func (f *fakeEvolution) CreateInstance(_ context.Context, name, token string) (evolution.Instance, error) {
	f.record("create", token)
	if f.createErr != nil {
		return evolution.Instance{}, f.createErr
	}
	created := evolution.Instance{ID: "new-" + name, Name: name, Status: evolution.StatusDisconnected}
	f.mu.Lock()
	f.instances = append(f.instances, created)
	f.mu.Unlock()
	return created, nil
}
func (f *fakeEvolution) DeleteInstance(_ context.Context, id string) error {
	f.record("delete", id)
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.instances[:0]
	for _, instance := range f.instances {
		if instance.ID != id {
			kept = append(kept, instance)
		}
	}
	f.instances = kept
	return nil
}
func (f *fakeEvolution) ConnectInstance(_ context.Context, token string) error {
	f.record("connect", token)
	return nil
}
func (f *fakeEvolution) DisconnectInstance(_ context.Context, token string) error {
	f.record("disconnect", token)
	return nil
}
func (f *fakeEvolution) LogoutInstance(_ context.Context, token string) error {
	f.record("logout", token)
	return nil
}
func (f *fakeEvolution) RequestHistory(_ context.Context, token string, anchor evolution.Anchor, count int) error {
	f.record("history", token)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.anchors = append(f.anchors, anchor)
	return f.historyErr
}
func (f *fakeEvolution) QRCode(_ context.Context, token string) (evolution.QRCode, error) {
	f.record("qr", token)
	return f.qr, f.qrErr
}

func testSessionKey() []byte { return []byte("test-only-session-key-that-is-long-enough") }

// signedIn starts a panel with an existing administrator and returns a client
// that already holds a valid session.
func signedIn(t *testing.T, repo *fakeRepo, evo *fakeEvolution) (*httptest.Server, *http.Client) {
	t.Helper()
	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	repo.user, repo.hash = "admin", hash
	repo.mu.Unlock()
	ts := httptest.NewServer(NewWebHandler(repo, evo, health.NewState(), testSessionKey(), "https://mcp.example"))
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

func fetch(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	if client == nil {
		client = &http.Client{}
	}
	r, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustContain(t *testing.T, page, name string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(page, want) {
			t.Errorf("%s page is missing %q", name, want)
		}
	}
}

func mustNotContain(t *testing.T, page, name string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(page, bad) {
			t.Errorf("%s page must not mention %q", name, bad)
		}
	}
}

func TestSetupCreatesOnlyOneAdminAndLoginWorks(t *testing.T) {
	repo := newRepo()
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example"))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	post := func(path string, form url.Values) *http.Response {
		r, e := client.PostForm(ts.URL+path, form)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := post("/setup", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if r.Request.URL.Path != "/" {
		t.Fatalf("setup ended at %s", r.Request.URL.Path)
	}
	r.Body.Close()
	if err := repo.CreateAdmin(context.Background(), "other", "hash"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second admin: %v", err)
	}
	post("/logout", nil).Body.Close()
	r = post("/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if r.Request.URL.Path != "/" {
		t.Fatalf("login ended at %s", r.Request.URL.Path)
	}
	r.Body.Close()
}

// The panel replaces the Evolution Manager, so no page may link to it or name
// the internal dependencies the operator is not supposed to know about.
func TestPanelNeverExposesEvolutionOrInternals(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	repo.selected = "one"
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	ts, client := signedIn(t, repo, evo)
	for _, path := range []string{"/", "/pair"} {
		page := fetch(t, client, ts.URL+path)
		mustNotContain(t, page, path, "EVOLUTION_URL", "EVOLUTION_API_KEY", "DATABASE_URL", "RABBITMQ", "manager/login", "swagger", "Evolution Manager", "Evolution Go")
	}
}

func TestEveryPageInlinesTheBrandLogo(t *testing.T) {
	logo := string(brand.LogoSVG())
	if !strings.HasPrefix(logo, "<svg") {
		t.Fatalf("brand.LogoSVG is not inline SVG: %q", logo)
	}
	fresh := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example"))
	defer fresh.Close()
	mustContain(t, fetch(t, nil, fresh.URL+"/setup"), "setup", logo, "Configuração inicial", `name="username"`, `name="password"`)

	repo := newRepo()
	evo := &fakeEvolution{}
	ts, client := signedIn(t, repo, evo)
	mustContain(t, fetch(t, nil, ts.URL+"/login"), "login", logo, `name="username"`, `name="password"`)
	mustContain(t, fetch(t, client, ts.URL+"/"), "dashboard", logo)
}

// Creating an instance must mint a token, register it with Evolution, store it,
// select it, start the client and send the operator to the pairing page.
func TestCreateInstanceRegistersTokenAndStartsPairing(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/instancias", url.Values{"name": {"pessoal"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Request.URL.Path != "/pair" {
		t.Fatalf("create ended at %s", r.Request.URL.Path)
	}
	if repo.selected != "new-pessoal" {
		t.Fatalf("selected = %q", repo.selected)
	}
	token, err := repo.InstanceToken(context.Background(), "new-pessoal")
	if err != nil || len(token) != 48 {
		t.Fatalf("token = %q err = %v", token, err)
	}
	if !evo.did("create") || !evo.did("connect") {
		t.Fatalf("calls = %v", evo.calls)
	}
	for _, used := range evo.tokens {
		if used != token {
			t.Fatalf("call used token %q, stored %q", used, token)
		}
	}
}

// A refused creation must leave nothing behind and must show Evolution's own
// reason, without stranding a half-created instance in the panel.
func TestCreateInstanceFailureKeepsPanelClean(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{createErr: errors.New("licença expirada")}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/instancias", url.Values{"name": {"pessoal"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if repo.selected != "" || len(repo.tokens) != 0 {
		t.Fatalf("panel kept state: selected=%q tokens=%v", repo.selected, repo.tokens)
	}
	mustContain(t, string(body), "dashboard", "licença expirada")
}

// The pairing page renders the QR as an inline image and keeps refreshing on
// its own, so pairing works without any client-side scripting.
func TestPairPageRendersQRCodeAndSelfRefreshes(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{
		instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusDisconnected}},
		qr:        evolution.QRCode{Image: "data:image/png;base64,AAAA", Code: "2@abc"},
	}
	ts, client := signedIn(t, repo, evo)
	page := fetch(t, client, ts.URL+"/pair")
	mustContain(t, page, "pair", `http-equiv="refresh"`, `src="data:image/png;base64,AAAA"`, "Dispositivos conectados", `class="guide"`)
	// The pairing code is for the camera, not for the operator to read.
	mustNotContain(t, page, "pair", "2@abc")
	if evo.tokens[len(evo.tokens)-1] != "tok" {
		t.Fatalf("QR fetched with token %q", evo.tokens)
	}
}

// Once the instance is connected there is nothing left to pair, so the page
// hands the operator back to the dashboard.
func TestPairPageRedirectsWhenConnected(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)
	r, err := client.Get(ts.URL + "/pair")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Request.URL.Path != "/instancias" {
		t.Fatalf("pair page ended at %s", r.Request.URL.Path)
	}
}

// Disconnect, logout and delete must all act on the selected instance using its
// stored token, and delete must clear the local record and the selection.
func TestInstanceActionsUseTheStoredToken(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	for _, action := range []string{"/instancias/desconectar", "/instancias/sair"} {
		r, err := client.PostForm(ts.URL+action, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
	}
	if !evo.did("disconnect") || !evo.did("logout") {
		t.Fatalf("calls = %v", evo.calls)
	}

	r, err := client.PostForm(ts.URL+"/instancias/remover", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if !evo.did("delete") {
		t.Fatalf("delete missing: %v", evo.calls)
	}
	if repo.selected != "" {
		t.Fatalf("selection survived delete: %q", repo.selected)
	}
	if _, err := repo.InstanceToken(context.Background(), "one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token survived delete: %v", err)
	}
}

// Every row removes itself, so the form names the instance and the selection
// only moves when the removed instance was the one in use.
func TestRemovingANamedInstanceLeavesTheSelectionAlone(t *testing.T) {
	repo := newRepo()
	for _, id := range []string{"one", "two"} {
		if err := repo.SaveInstance(context.Background(), id, id, "tok-"+id); err != nil {
			t.Fatal(err)
		}
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{
		{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected},
		{ID: "two", Name: "Trabalho", Status: evolution.StatusDisconnected},
	}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/instancias/remover", url.Values{"instance_id": {"two"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if _, err := repo.InstanceToken(context.Background(), "two"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token survived delete: %v", err)
	}
	if repo.selected != "one" {
		t.Fatalf("selection changed: %q", repo.selected)
	}
	if _, err := repo.InstanceToken(context.Background(), "one"); err != nil {
		t.Fatalf("the instance in use was touched: %v", err)
	}
}

// An instance created outside this panel has no token here, so the panel must
// refuse to operate it instead of failing obscurely against Evolution.
func TestUnmanagedInstanceCannotBeOperated(t *testing.T) {
	repo := newRepo()
	repo.selected = "foreign"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "foreign", Name: "De fora", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/instancias/desconectar", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if evo.did("disconnect") {
		t.Fatal("panel operated an instance it does not manage")
	}
	mustContain(t, string(body), "instances", "instância deste painel")
	mustContain(t, fetch(t, client, ts.URL+"/instancias"), "instances", "Sem credenciais aqui")
}

func TestSelectionAllowsOnlyListedSingleInstance(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{instances: []evolution.Instance{
		{ID: "one", Name: "Um", Status: evolution.StatusConnected},
		{ID: "two", Name: "Dois", Status: evolution.StatusDisconnected},
	}}
	ts, client := signedIn(t, repo, evo)

	r, _ := client.PostForm(ts.URL+"/instancias/selecionar", url.Values{"instance_id": {"one"}, "instance_id_extra": {"two"}})
	r.Body.Close()
	if repo.selected != "one" {
		t.Fatalf("selected=%q", repo.selected)
	}
	r, _ = client.PostForm(ts.URL+"/instancias/selecionar", url.Values{"instance_id": {"unknown"}})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown status=%d", r.StatusCode)
	}
	r.Body.Close()
	r, _ = client.Get(ts.URL + "/api/selected-instance")
	if r.StatusCode != http.StatusOK {
		t.Fatal(r.Status)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), `"status":"connected"`) {
		t.Fatal(string(b))
	}
}

// The pairing QR arrives as a data: URI, so the policy must allow it for images
// and for nothing else.
func TestContentSecurityPolicyAllowsInlineQRImages(t *testing.T) {
	ts := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example"))
	defer ts.Close()
	r, err := http.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	policy := r.Header.Get("Content-Security-Policy")
	mustContain(t, policy, "CSP", "default-src 'self'", "img-src 'self' data:")
}

// The QR source is remote input rendered into a src attribute, so only the
// documented inline PNG survives.
func TestQRImageSourceAcceptsOnlyInlinePNG(t *testing.T) {
	good := "data:image/png;base64,iVBORw0KGgo="
	if got := qrImageSource(good); string(got) != good {
		t.Fatalf("valid QR rejected: %q", got)
	}
	for _, bad := range []string{
		"",
		"javascript:alert(1)",
		"https://example.com/qr.png",
		"data:text/html;base64,PHNjcmlwdD4=",
		"data:image/png;base64,",
		"data:image/png;base64,not valid base64!!",
	} {
		if got := qrImageSource(bad); got != "" {
			t.Errorf("qrImageSource(%q) = %q, want empty", bad, got)
		}
	}
}

// The panel is where the operator looks when something breaks, so the status
// card has to name the failure, the session state and the queue that stopped.
func TestDashboardShowsOperationalStatus(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	repo.coverage = store.Coverage{Messages: 42, OldestAt: time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)}
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}

	state := health.NewState()
	state.SetDependencies(true, false, true)
	state.SetWhatsApp("logged_out", "401: logged out from another device", "", "Bruno")
	state.SetQueueConsuming("message", true, "")
	state.MarkQueueEvent("message", time.Now(), false)
	state.SetQueueConsuming("historysync", false, "channel closed")

	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo.user, repo.hash = "admin", hash
	ts := httptest.NewServer(NewWebHandler(repo, evo, state, testSessionKey(), "https://mcp.example"))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()

	page := fetch(t, client, ts.URL+"/estado")
	mustContain(t, page, "status page",
		"Estado do serviço", "Sessão encerrada",
		"a sessão do WhatsApp foi encerrada e exige um novo QR code",
		"a fila de eventos está inacessível",
		"a fila historysync não está sendo consumida",
		"401: logged out from another device",
		"Bruno", "42", "04/03/2026",
		"message", "Consumindo", "Parada")
}

// The secret must appear exactly once, in the body of the response that creates
// it, and never in a URL: a query string lands in browser history, proxy logs
// and the referrer of the next request.
func TestCreatingAKeyShowsItOnceAndNeverInAURL(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"claude code"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	page := string(body)

	if r.Request.URL.RawQuery != "" {
		t.Fatalf("the secret round-tripped through a URL: %s", r.Request.URL)
	}
	// The brand SVG defines a gradient id that also starts with the prefix, so
	// the secret is read from where it is actually offered to the client.
	index := strings.Index(page, "Bearer "+store.KeyPrefix)
	if index < 0 {
		t.Fatalf("the new key was not shown in a usable configuration: %s", page)
	}
	index += len("Bearer ")
	secret := page[index : index+len(store.KeyPrefix)+24]
	if len(repo.keys) != 1 || repo.keys[0].InstanceID != "one" {
		t.Fatalf("key was not stored against the instance: %+v", repo.keys)
	}
	// Only the digest is stored, never the secret itself.
	if repo.digests[0] == secret || repo.digests[0] != store.HashAPIKey(secret) {
		t.Fatal("the stored value is not the digest of the secret")
	}
	// The snippets must arrive usable: the endpoint, the secret in place, the
	// one-line client command and the JSON block for clients configured by file.
	mustContain(t, page, "key page", "https://mcp.example/mcp", "Bearer "+secret, "claude mcp add", "mcpServers", "WHATSAPP_MCP_KEY")

	// Reloading must not repeat the secret.
	reloaded := fetch(t, client, ts.URL+"/")
	if strings.Contains(reloaded, secret) {
		t.Fatal("the secret is shown again after a reload")
	}
	mustContain(t, reloaded, "dashboard", "claude code", repo.keys[0].Prefix)
}

func TestRevokingAKeyRemovesIt(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, _ := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"temporária"}})
	r.Body.Close()
	if len(repo.keys) != 1 {
		t.Fatalf("keys = %+v", repo.keys)
	}
	r, _ = client.PostForm(ts.URL+"/chaves/revogar", url.Values{"id": {"1"}})
	r.Body.Close()
	if len(repo.keys) != 0 {
		t.Fatalf("key survived revocation: %+v", repo.keys)
	}
}

// The history request pages backwards from a message the index already holds,
// which is what the WhatsApp protocol requires.
func TestHistorySyncAnchorsOnTheOldestMessage(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	repo.oldest = store.Message{MessageID: "OLD", ChatJID: "a@s.whatsapp.net", SentAt: time.Now().Add(-48 * time.Hour)}
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/instancias/historico", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if len(evo.anchors) != 1 || evo.anchors[0].MessageID != "OLD" {
		t.Fatalf("anchors = %+v", evo.anchors)
	}

	// With nothing indexed there is no anchor, and the panel has to say why
	// rather than fail silently.
	empty := newRepo()
	if err := empty.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	empty.selected = "one"
	empty.oldestErr = errors.New("no rows")
	ts2, client2 := signedIn(t, empty, &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}})
	r, _ = client2.PostForm(ts2.URL+"/instancias/historico", nil)
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "history failure", "nenhuma mensagem indexada")
}

// The panel is split by task, so each page must exist on its own and the tab
// bar must say where the operator is.
func TestPagesAreSeparateAndTheTabBarTracksThem(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	for _, page := range []struct {
		path, heading, current string
		absent                 []string
	}{
		{"/", "Conectar um cliente", `href="/" aria-current="page"`, []string{"Adicionar instância", "Filas de ingestão"}},
		{"/instancias", "Instâncias", `href="/instancias" aria-current="page"`, []string{"Chaves ativas", "Filas de ingestão"}},
		{"/estado", "Estado do serviço", `href="/estado" aria-current="page"`, []string{"Chaves ativas", "Adicionar instância"}},
	} {
		body := fetch(t, client, ts.URL+page.path)
		mustContain(t, body, page.path, page.heading, page.current, `href="/instancias"`, `href="/estado"`)
		mustNotContain(t, body, page.path, page.absent...)
		// The theme switch rides in the masthead, and the script that applies
		// the choice loads before the first paint.
		mustContain(t, body, page.path, "data-theme-select", `src="/assets/theme.js"`)
	}
}

// The dark palette must reach both the system preference and an explicit pick,
// or choosing a theme would only change half the page.
func TestDarkPaletteServesBothTheSystemAndAnExplicitChoice(t *testing.T) {
	repo := newRepo()
	ts, client := signedIn(t, repo, &fakeEvolution{})
	body := fetch(t, client, ts.URL+"/")
	mustContain(t, body, "connect", "@media (prefers-color-scheme:dark){:root:not([data-theme=light])", ":root[data-theme=dark]{color-scheme:dark;")
	if strings.Count(body, "--brand-ink:#04211b") != 2 {
		t.Fatalf("the dark palette is not applied to both selectors")
	}
	mustNotContain(t, body, "connect", darkMarker)
}

// Creating something happens in a dialog, and the dialog is plain markup so it
// works with the panel's content security policy and without scripting.
func TestDialogsAreMarkupOnly(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	connect := fetch(t, client, ts.URL+"/")
	mustContain(t, connect, "connect", `id="nova-chave"`, `href="#nova-chave"`, `action="/chaves"`, "required")

	instances := fetch(t, client, ts.URL+"/instancias")
	mustContain(t, instances, "instances", `id="nova-instancia"`, `id="remover-0"`, `id="encerrar-sessao"`, `action="/instancias"`)
	// Destructive actions must be confirmed rather than fired by a stray click.
	mustContain(t, instances, "instances", "Remover a instância?", "Encerrar a sessão do WhatsApp?")
	// The confirmation names the account it destroys, and the row that opens it
	// carries the instance id, so neither depends on what is selected.
	mustContain(t, instances, "instances", `href="#remover-0"`, `name="instance_id" value="one"`, "Pessoal")
	// Slow forms say the click landed instead of looking idle.
	mustContain(t, instances, "instances", `data-busy="Criando instância…"`, "data-busy-note")
}

// A key without a name is a key nobody can identify later, which is what made
// the old flow confusing. It is refused rather than silently named.
func TestKeyRequiresAName(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"   "}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if len(repo.keys) != 0 {
		t.Fatalf("an unnamed key was created: %+v", repo.keys)
	}
	mustContain(t, string(body), "connect", "Dê um nome")
}

// The copy helper is served from the panel itself, which is what lets the
// content security policy stay at 'self'.
func TestPanelServesItsOwnScript(t *testing.T) {
	ts := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example"))
	defer ts.Close()
	r, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", r.StatusCode)
	}
	if !strings.Contains(string(body), "clipboard") {
		t.Fatalf("unexpected asset body: %s", body)
	}
	// The theme override ships the same way, from this origin.
	theme, err := http.Get(ts.URL + "/assets/theme.js")
	if err != nil {
		t.Fatal(err)
	}
	themeBody, _ := io.ReadAll(theme.Body)
	theme.Body.Close()
	if theme.StatusCode != http.StatusOK || !strings.Contains(string(themeBody), "data-theme") {
		t.Fatalf("theme asset status = %d body = %s", theme.StatusCode, themeBody)
	}
	// A directory listing would expose the layout of the embedded files.
	listing, err := http.Get(ts.URL + "/assets/")
	if err != nil {
		t.Fatal(err)
	}
	listing.Body.Close()
	if listing.StatusCode == http.StatusOK {
		t.Fatal("the asset directory is listable")
	}
}

func TestCountAndPluralReadNaturally(t *testing.T) {
	if got := plural(1, "evento", "eventos"); got != "1 evento" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(2, "evento", "eventos"); got != "2 eventos" {
		t.Errorf("plural(2) = %q", got)
	}
	for quantity, want := range map[int64]string{0: "0", 999: "999", 1000: "1.000", 18432: "18.432", 1234567: "1.234.567"} {
		if got := count(quantity); got != want {
			t.Errorf("count(%d) = %q, want %q", quantity, got, want)
		}
	}
}

// The checklist reflects facts the panel already holds rather than a stored
// notion of progress, so revoking the last key reopens the first step on its
// own and a key that has been used proves the client is configured.
func TestConnectChecklistTracksTheRealState(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	// No key yet: the first step is open and asks for one.
	page := fetch(t, client, ts.URL+"/")
	mustContain(t, page, "checklist empty", "Gerar nova chave")
	mustNotContain(t, page, "checklist empty", `class="step step--done"`, "concluído")

	// A key exists but was never used: step one is done, step two is not.
	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"notebook"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "checklist with key", `class="step step--done"`, "concluído", "1 chave ativa", "Gerar outra chave")
	mustNotContain(t, page, "checklist with key", "Um cliente se autenticou")

	// The key has been used: a client authenticated, so the remaining steps close.
	repo.mu.Lock()
	repo.keys[0].LastUsedAt = time.Now().Add(-5 * time.Minute)
	repo.mu.Unlock()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "checklist used", "Um cliente se autenticou", "pronto")

	// Revoking the last key reopens the first step without any extra bookkeeping.
	r, err = client.PostForm(ts.URL+"/chaves/revogar", url.Values{"id": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "checklist after revoke", "Gerar nova chave")
	mustNotContain(t, page, "checklist after revoke", `class="step step--done"`, "concluído")
}

// The connect page notices a client authenticating without a manual reload, so
// the checklist has to be readable as data. It must say nothing beyond that.
func TestProgressEndpointReportsTheChecklistAndNothingElse(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	read := func() map[string]any {
		t.Helper()
		r, err := client.Get(ts.URL + "/api/progresso")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", r.StatusCode)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	state := read()
	if state["has_key"] != false || state["client_connected"] != false {
		t.Fatalf("empty state = %#v", state)
	}

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"notebook"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if state = read(); state["has_key"] != true || state["client_connected"] != false {
		t.Fatalf("with key = %#v", state)
	}

	repo.mu.Lock()
	repo.keys[0].LastUsedAt = time.Now()
	repo.mu.Unlock()
	if state = read(); state["client_connected"] != true {
		t.Fatalf("after use = %#v", state)
	}
	// The checklist is the whole payload: no credential, no instance, no prefix.
	if len(state) != 2 {
		t.Fatalf("progress leaked extra fields: %#v", state)
	}

	// It is a signed-in view like any other page.
	anonymous, err := http.Get(ts.URL + "/api/progresso")
	if err != nil {
		t.Fatal(err)
	}
	anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", anonymous.StatusCode)
	}
}
