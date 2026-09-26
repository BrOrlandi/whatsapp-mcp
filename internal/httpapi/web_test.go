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
	mustChange           bool
	license              store.EvolutionLicense
	operatorEmail        string
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
func (f *fakeRepo) AdminMustChangePassword(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mustChange, nil
}
func (f *fakeRepo) SetAdminPassword(_ context.Context, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hash, f.mustChange = h, false
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
func (f *fakeRepo) SaveOperatorEmail(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The real store writes the address into the same singleton row the
	// licence lives in, so an address saved before any activation is readable
	// on its own. The fake has to do the same or the wizard's waiting state
	// would never be reachable in a test.
	f.operatorEmail, f.license.OperatorEmail = email, email
	return nil
}
func (f *fakeRepo) MarkLicenseLinkSent(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operatorEmail, f.license.OperatorEmail = email, email
	f.license.LinkSentAt = time.Now()
	return nil
}
func (f *fakeRepo) SaveEvolutionLicense(_ context.Context, license store.EvolutionLicense) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The real store clears the pending link, because the click that answered
	// it is what got us here.
	f.license = license
	return nil
}
func (f *fakeRepo) EvolutionLicense(context.Context) (store.EvolutionLicense, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.license.APIKey == "" && f.license.OperatorEmail == "" {
		return store.EvolutionLicense{}, store.ErrNoLicense
	}
	return f.license, nil
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
	license    evolution.License
	licenseErr error

	registerErr   error
	operatorEmail string
	operatorName  string
	activation    evolution.LicenseActivation
	activationErr error
	healErr       error
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
func (f *fakeEvolution) License(context.Context, string) (evolution.License, error) {
	return f.license, f.licenseErr
}
func (f *fakeEvolution) RegisterOperator(_ context.Context, email, name, callback string) error {
	f.record("register-operator", email+" "+name+" "+callback)
	if f.registerErr != nil {
		return f.registerErr
	}
	f.mu.Lock()
	f.operatorEmail, f.operatorName = email, name
	f.mu.Unlock()
	return nil
}
func (f *fakeEvolution) CompleteActivation(_ context.Context, code string) (evolution.LicenseActivation, error) {
	f.record("complete-activation", code)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.activationErr != nil {
		return evolution.LicenseActivation{}, f.activationErr
	}
	return f.activation, nil
}
func (f *fakeEvolution) ReactivateLicense(_ context.Context, apiKey string) error {
	f.record("reactivate", apiKey)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.healErr != nil {
		return f.healErr
	}
	// A licence that went back in unblocks the instance API, which is what the
	// handler re-reads right after healing.
	f.err = nil
	f.license = evolution.License{Status: "active"}
	return nil
}
func (f *fakeEvolution) QRCode(_ context.Context, token string) (evolution.QRCode, error) {
	f.record("qr", token)
	return f.qr, f.qrErr
}

func testSessionKey() []byte { return []byte("test-only-session-key-that-is-long-enough") }

// signedIn starts a panel with an existing administrator and returns a client
// that already holds a valid session.
// signedInAutoWait is how long the automatic licence path is given in tests.
// A test that wants the stalled fallback shortens it with shortAutoWait.
var signedInAutoWait = 3 * time.Minute

// shortAutoWait makes the automatic wait expire immediately, for the tests
// about what the wizard does once it has.
func shortAutoWait(t *testing.T) {
	t.Helper()
	previous := signedInAutoWait
	signedInAutoWait = time.Nanosecond
	t.Cleanup(func() { signedInAutoWait = previous })
}

func signedIn(t *testing.T, repo *fakeRepo, evo *fakeEvolution, licenseAuto ...bool) (*httptest.Server, *http.Client) {
	t.Helper()
	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	repo.user, repo.hash = "admin", hash
	repo.mu.Unlock()
	auto := false
	for _, want := range licenseAuto {
		auto = want
	}
	ts := httptest.NewServer(NewWebHandler(repo, evo, health.NewState(), testSessionKey(), "https://mcp.example", "", auto, "brorlandi.xyz", signedInAutoWait, nil))
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
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
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
	// The identity is an address, and it has to be one: the next step of the
	// installation is a link sent to it.
	r := post("/setup", url.Values{"email": {"admin"}, "password": {"senha segura 123"}})
	rejected, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if repo.user != "" {
		t.Fatalf("an administrator was created under %q, which is not an address", repo.user)
	}
	mustContain(t, string(rejected), "setup", "Informe um e-mail válido")

	// An installation that has never been finished lands on the wizard rather
	// than on a panel with nothing in it.
	r = post("/setup", url.Values{"email": {"bruno@example.com"}, "password": {"senha segura 123"}})
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("setup ended at %s", r.Request.URL.Path)
	}
	r.Body.Close()
	if err := repo.CreateAdmin(context.Background(), "other", "hash"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second admin: %v", err)
	}
	post("/logout", nil).Body.Close()
	// The administrator signs in with the address they were created under.
	r = post("/login", url.Values{"username": {"bruno@example.com"}, "password": {"senha segura 123"}})
	if r.Request.URL.Path != "/instalacao" {
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
	fresh := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer fresh.Close()
	mustContain(t, fetch(t, nil, fresh.URL+"/setup"), "setup", logo, "Configuração inicial", `name="email"`, `name="password"`)

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
	ts := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
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
	ts := httptest.NewServer(NewWebHandler(repo, evo, state, testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
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
		{"/", "Seu WhatsApp nas suas ferramentas de IA", `href="/" aria-current="page"`, []string{"Adicionar instância", "Filas de ingestão"}},
		{"/instancias", "Instâncias", `href="/instancias" aria-current="page"`, []string{"Suas conexões", "Filas de ingestão"}},
		{"/estado", "Estado do serviço", `href="/estado" aria-current="page"`, []string{"Suas conexões", "Adicionar instância"}},
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
	mustContain(t, connect, "connect", `id="nova-conexao"`, `href="#nova-conexao"`, `action="/chaves"`, `name="cliente"`)

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

// Naming a credential is work the panel can do itself. The operator answers
// "where are you going to use this?" and that answer becomes the connection's
// name, so nothing is refused for being unnamed; only a label too long to fit a
// row is.
func TestAConnectionNamesItselfAfterTheChosenTool(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"code"}, "name": {"   "}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if len(repo.keys) != 1 || repo.keys[0].Name != "Claude Code" {
		t.Fatalf("the connection was not named after the chosen tool: %+v", repo.keys)
	}
	// The instructions open on the tool that was picked, not on the first tab.
	mustContain(t, string(body), "key page", `id="tab-code" checked`)

	// A label the operator does type is kept, and one that cannot fit a row is
	// refused rather than truncated.
	r, err = client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"desktop"}, "name": {strings.Repeat("x", 61)}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if len(repo.keys) != 1 {
		t.Fatalf("an over-long label was accepted: %+v", repo.keys)
	}
	mustContain(t, string(body), "connect", "até 60 caracteres")
}

// The copy helper is served from the panel itself, which is what lets the
// content security policy stay at 'self'.
func TestPanelServesItsOwnScript(t *testing.T) {
	ts := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
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

// The landing page talks about tools, not credentials, and it works that out
// from facts it already holds: a connection exists or it does not, a connection
// that has been used is one that works, and a client that announced itself in
// the MCP handshake is named by that instead of by the label typed here.
func TestTheLandingPageDescribesConnectionsRatherThanKeys(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Number: "5511923456789:89", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	// Nothing connected yet: the page says so in those words and offers the one
	// action that changes it. No phone number is left in its protocol shape.
	page := fetch(t, client, ts.URL+"/")
	mustContain(t, page, "no connection", "Nenhuma ferramenta de IA conectada", "Conectar uma ferramenta de IA", "55 (11) 92345-6789")
	mustNotContain(t, page, "no connection", "5511923456789", "Chaves ativas", "Gerar nova chave")

	// A connection exists but has never been used: it is waiting, not working.
	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"desktop"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "waiting", "Esperando a sua ferramenta de IA", "Aguardando", "Claude Desktop", `data-connected="false"`)

	// Once it has been used the page counts it, and the tool's own name from the
	// MCP handshake wins over the label this panel chose.
	repo.mu.Lock()
	repo.keys[0].LastUsedAt = time.Now().Add(-5 * time.Minute)
	repo.keys[0].ClientName = "claude-ai"
	repo.mu.Unlock()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "connected", "1 ferramenta de IA conectada", "Conectada", "Claude Desktop", "Experimente pedir")
	mustNotContain(t, page, "connected", `data-connected="false"`)

	// Disconnecting is a confirmed action, and it takes the page back to empty.
	mustContain(t, page, "connected", `id="desconectar-0"`, "Desconectar")
	r, err = client.PostForm(ts.URL+"/chaves/revogar", url.Values{"id": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	page = fetch(t, client, ts.URL+"/")
	mustContain(t, page, "after disconnect", "Nenhuma ferramenta de IA conectada")
}

// The setup instructions lead with the client that needs no terminal, and they
// carry a route for every other tool: a message the assistant itself reads.
func TestSetupOffersDesktopFirstAndAnEscapeHatchForEveryOtherTool(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}
	ts, client := signedIn(t, repo, evo)

	r, err := client.PostForm(ts.URL+"/chaves", url.Values{"cliente": {"outros"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	page := string(body)

	// Desktop is the first tab on the bar whichever one opens.
	desktop := strings.Index(page, `for="tab-desktop"`)
	code := strings.Index(page, `for="tab-code"`)
	others := strings.Index(page, `for="tab-outros"`)
	if desktop < 0 || code < 0 || others < 0 || !(desktop < code && code < others) {
		t.Fatalf("the client tabs are not in the order desktop, code, outros: %d %d %d", desktop, code, others)
	}
	// The chosen route is the one that opens, and it hands over a prompt the
	// assistant can act on rather than instructions the operator must translate.
	mustContain(t, page, "key page", `id="tab-outros" checked`, "Quero conectar um servidor MCP", "https://mcp.example/mcp")
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

// The tab icon is embedded and served by the panel itself, before anyone has
// logged in: a browser asks for it on the login page, and an icon behind the
// session cookie would just 302 into the login form forever.
func TestPanelServesItsOwnIcons(t *testing.T) {
	ts := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	for path, wantType := range map[string]string{
		"/favicon.svg":          "image/svg+xml",
		"/favicon.ico":          "image/x-icon",
		"/apple-touch-icon.png": "image/png",
	} {
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d", path, r.StatusCode)
		}
		if got := r.Header.Get("Content-Type"); got != wantType {
			t.Fatalf("%s: content type = %q, want %q", path, got, wantType)
		}
		if len(body) == 0 {
			t.Fatalf("%s: empty body", path)
		}
	}
	// The pages must point at them, or the browser falls back to guessing.
	page := fetch(t, ts.Client(), ts.URL+"/login")
	mustContain(t, page, "login", `rel="icon" href="/favicon.svg"`, `rel="apple-touch-icon"`)
}

// An installer invents the first password and prints it to a terminal, where it
// survives in scrollback and shell history. Until it is replaced, the panel must
// answer nothing else: a leaked bootstrap password should buy an attacker a
// password form, not a WhatsApp session.
func TestBootstrapPasswordMustBeReplacedBeforeAnythingElse(t *testing.T) {
	repo := newRepo()
	repo.mustChange = true
	evo := &fakeEvolution{}
	ts, client := signedIn(t, repo, evo)

	for _, path := range []string{"/", "/instancias", "/estado", "/documentacao"} {
		r, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.Request.URL.Path != "/senha" {
			t.Fatalf("%s landed on %s, want /senha", path, r.Request.URL.Path)
		}
	}

	// The current password is required even though the session already proves
	// who this is, so a session left open cannot lock the owner out.
	wrong := url.Values{"current_password": {"not the password"}, "password": {"uma senha nova"}, "confirm_password": {"uma senha nova"}}
	r, err := client.PostForm(ts.URL+"/senha", wrong)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong current password returned %d", r.StatusCode)
	}

	right := url.Values{"current_password": {"senha segura 123"}, "password": {"uma senha nova"}, "confirm_password": {"uma senha nova"}}
	r, err = client.PostForm(ts.URL+"/senha", right)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("after the change the panel landed on %s", r.Request.URL.Path)
	}
	if repo.mustChange {
		t.Fatal("the rotation flag survived the change")
	}
}

// Changing the password when nothing is forcing it has to be possible and has
// to be findable: a panel where the only way to rotate the administrator
// password is to guess a URL does not really let you rotate it.
func TestPasswordCanBeChangedOnPurpose(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{}
	ts, client := signedIn(t, repo, evo)

	// The masthead offers the way in, on every page that carries it.
	mustContain(t, fetch(t, client, ts.URL+"/instancias"), "instances", `href="/senha"`)

	page := fetch(t, client, ts.URL+"/senha")
	mustContain(t, page, "senha", "Trocar a senha", `name="current_password"`)
	if strings.Contains(page, "gerada pelo instalador") {
		t.Fatal("a voluntary change is described as the installer's forced one")
	}

	r, err := client.PostForm(ts.URL+"/senha", url.Values{
		"current_password": {"senha segura 123"},
		"password":         {"outra senha bem boa"},
		"confirm_password": {"outra senha bem boa"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "senha", "Senha alterada.")

	// The new one works and the old one does not.
	fresh := &http.Client{Jar: newJar(t)}
	login := func(c *http.Client, password string) int {
		resp, err := c.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {password}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := login(fresh, "senha segura 123"); code != http.StatusUnauthorized {
		t.Fatalf("the old password still signs in: %d", code)
	}
	if code := login(&http.Client{Jar: newJar(t)}, "outra senha bem boa"); code != http.StatusOK {
		t.Fatalf("the new password does not sign in: %d", code)
	}
}

func newJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

// The installer publishes a URL to the internet, so "create the administrator"
// cannot be open to whoever reaches it first. The token in the link the
// installer printed is what closes that window — and the link is the only way
// in, so the form is not even offered without it.
func TestSetupNeedsTheInstallerToken(t *testing.T) {
	const token = "9f2c1ab4d0e7"
	repo := newRepo()
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", token, false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	client := &http.Client{Jar: newJar(t)}

	// No token: no form at all.
	r, err := client.Get(ts.URL + "/setup")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("bare /setup returned %d", r.StatusCode)
	}
	if strings.Contains(string(body), `name="username"`) {
		t.Fatal("the form was offered without the token")
	}

	// A wrong token is refused the same way.
	r, err = client.Get(ts.URL + "/setup?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("a wrong token returned %d", r.StatusCode)
	}

	// Posting straight past the page is refused too.
	r, err = client.PostForm(ts.URL+"/setup", url.Values{"username": {"intruso"}, "password": {"uma senha longa"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without the token returned %d", r.StatusCode)
	}
	if repo.user != "" {
		t.Fatalf("an administrator was created without the token: %q", repo.user)
	}

	// The link works, and the operator picks both the address and the password.
	page := fetch(t, client, ts.URL+"/setup?token="+token)
	mustContain(t, page, "setup", `name="email"`, `name="setup_token"`, token)

	r, err = client.PostForm(ts.URL+"/setup", url.Values{
		"setup_token": {token}, "email": {"bruno@example.com"}, "password": {"uma senha bem longa"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if repo.user != "bruno@example.com" {
		t.Fatalf("administrator = %q, want the address the operator typed", repo.user)
	}
	// And it is a real sign-in, not a password waiting to be replaced.
	if must, _ := repo.AdminMustChangePassword(context.Background()); must {
		t.Fatal("a password the operator chose was marked as needing a change")
	}
}

// An Evolution with no licence answers 503 on every route. That is not an
// outage and must not be reported as one: waiting changes nothing, so the page
// has to name the real cause and carry the link that fixes it.
func TestUnactivatedEvolutionIsNotReportedAsAnOutage(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
	}
	ts, client := signedIn(t, repo, evo)

	// A missing licence is the first step of the installation wizard, offered
	// as a form that can be filled here rather than a link into somebody
	// else's site.
	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "instalacao", "Ativar a licença", `action="/instancias/licenca"`, `name="email"`)
	if strings.Contains(page, "Tente novamente em instantes") {
		t.Fatal("a missing licence is described as a transient outage")
	}
	// None of it arrives as an alert: a deployment that was installed a minute
	// ago has no licence yet, and that is the normal starting state.
	mustNotContain(t, page, "instalacao", `class="alert"`, "503")

	// The panel itself only points at the wizard, so the activation form
	// exists in exactly one place.
	panel := fetch(t, client, ts.URL+"/instancias")
	mustContain(t, panel, "instancias", "ainda não foi ativada", `href="/instalacao"`)
	mustNotContain(t, panel, "instancias", `action="/instancias/licenca"`)

	// Creating an instance says the same thing rather than leaking the raw
	// Evolution error at the operator.
	evo.createErr = evolution.ErrNotActivated
	r, err := client.PostForm(ts.URL+"/instancias", url.Values{"name": {"pessoal"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "instancias", "ainda não foi ativada")
}

func TestLicenseRegistrationRunsThroughThePanel(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
		activation: evolution.LicenseActivation{
			APIKey: "evo-key-1", Tier: "evolution-go", CustomerID: 42, InstanceID: "inst-9",
		},
	}
	ts, client := signedIn(t, repo, evo)
	defer ts.Close()

	// The form is the way in: the operator never leaves the panel, and never
	// opens the licensing site. Only the email is typed — the name on the
	// registry is the product's, not a second field to fill.
	r, err := client.PostForm(ts.URL+"/instancias/licenca", url.Values{
		"email": {"bruno@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !evo.did("register-operator") {
		t.Fatal("the magic link was not requested through Evolution")
	}
	if evo.operatorEmail != "bruno@example.com" {
		t.Fatalf("register-operator handled email %q", evo.operatorEmail)
	}
	if got := evo.operatorName; got != brand.Name {
		t.Fatalf("register-operator handled name %q, want the product name %q", got, brand.Name)
	}
	// The send lands back on the wizard, which now waits on the inbox instead
	// of asking for the address again.
	mustContain(t, string(body), "instalacao", "bruno@example.com", "15 minutos", "Não recebeu o e-mail?")
	if repo.operatorEmail != "bruno@example.com" {
		t.Fatal("the operator email was not kept for the callback")
	}

	// The click comes back to the panel without a session behind it, because
	// it is the one-time code that carries the authority and because a mail
	// client opens links in a browser of its own. It must answer there rather
	// than send that visitor to a login form.
	bare := &http.Client{}
	r, err = bare.Get(ts.URL + "/instancias/licenca/retorno?code=one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("callback returned %d", r.StatusCode)
	}
	if r.Request.URL.Path != "/instancias/licenca/retorno" {
		t.Fatalf("a session-less click was sent to %s", r.Request.URL.Path)
	}
	mustContain(t, string(body), "callback", "Licença ativada")
	saved, err := repo.EvolutionLicense(context.Background())
	if err != nil {
		t.Fatalf("no licence was kept: %v", err)
	}
	if saved.APIKey != "evo-key-1" || saved.InstanceID != "inst-9" {
		t.Fatalf("kept licence = %+v", saved)
	}
}

func TestLicenseCallbackRefusesAnEmptyOrRejectedCode(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{activationErr: errors.New("licensing server said no")}
	ts, client := signedIn(t, repo, evo)
	defer ts.Close()

	// Registered instances stay put when the activation goes wrong; the
	// operator can retry the link or ask for another.
	r, err := client.Get(ts.URL + "/instancias/licenca/retorno?code=")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "instalacao", "veio sem o código")

	r, err = client.Get(ts.URL + "/instancias/licenca/retorno?code=spent")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "instalacao", "Não foi possível ativar a licença")

	// The licence form does not accept a nonsense e-mail either.
	r, err = client.PostForm(ts.URL+"/instancias/licenca", url.Values{"email": {"not-an-email"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if evo.did("register-operator") {
		t.Fatal("a magic link was requested for an invalid e-mail")
	}
}

func TestASavedLicenceReactivatesItself(t *testing.T) {
	repo := newRepo()
	repo.license = store.EvolutionLicense{OperatorEmail: "bruno@example.com", APIKey: "evo-key-1", Tier: "evolution-go"}
	evo := &fakeEvolution{
		err:       evolution.ErrNotActivated,
		instances: []evolution.Instance{{ID: "inst-1", Name: "pessoal", Status: evolution.StatusDisconnected, Token: "tok"}},
		license:   evolution.License{Status: "inactive"},
	}
	ts, client := signedIn(t, repo, evo)
	defer ts.Close()

	// Sign-in itself heals once Evolution loses its licence mid-session, which
	// consumes the fresh state. Losing it again is what the operator would
	// actually be looking at when the alert matters.
	evo.mu.Lock()
	evo.err, evo.license = evolution.ErrNotActivated, evolution.License{Status: "inactive"}
	evo.mu.Unlock()

	page := fetch(t, client, ts.URL+"/instancias")
	if !evo.did("reactivate") {
		t.Fatal("the panel did not hand Evolution the licence it was keeping")
	}
	mustContain(t, page, "instancias", "reativada automaticamente", "pessoal")
	if strings.Contains(page, "ainda não foi ativada") {
		t.Fatal("an operator was asked to register again although the panel had the licence")
	}

	// And the saved e-mail is offered as prefill when a heal cannot happen.
	repo2 := newRepo()
	repo2.license = store.EvolutionLicense{OperatorEmail: "bruno@example.com", APIKey: "evo-key-1", Tier: "evolution-go"}
	evo2 := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=def"},
		healErr: errors.New("revoked"),
	}
	ts2, client2 := signedIn(t, repo2, evo2)
	defer ts2.Close()
	page2 := fetch(t, client2, ts2.URL+"/instalacao")
	mustContain(t, page2, "instalacao", `value="bruno@example.com"`, "Ativar a licença")
}

// The first run is a wizard, not a panel full of red. A deployment that was
// installed a minute ago has no licence and no paired phone: that is the
// normal starting state, and the panel used to greet its owner with an outage
// banner on three pages at once.
func TestTheFirstRunIsAWizardAndNotAnOutage(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:        evolution.ErrNotActivated,
		license:    evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
		activation: evolution.LicenseActivation{APIKey: "evo-key-1", Tier: "evolution-go", InstanceID: "inst-9"},
	}
	ts, client := signedIn(t, repo, evo)

	// Signing in lands on the wizard rather than on a panel that cannot do
	// anything yet, and the wizard carries no tab bar to wander off into.
	r, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("a fresh installation landed on %s", r.Request.URL.Path)
	}
	licence := string(page)
	mustContain(t, licence, "wizard step 1", "Ativar a licença", `name="email"`, `class="wizard`, "wizard__step--now")
	mustNotContain(t, licence, "wizard step 1", `class="nav"`, `href="/estado"`, `href="/documentacao"`, `class="alert"`)

	// Sending the link turns the step into a wait on an inbox, with a way to
	// try another address for the operator whose mail never arrives.
	r, err = client.PostForm(ts.URL+"/instancias/licenca", url.Values{"email": {"bruno@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(r.Body)
	r.Body.Close()
	waiting := string(page)
	mustContain(t, waiting, "wizard waiting", "bruno@example.com", "Não recebeu o e-mail?", `data-onboarding="1"`, "Verificando a ativação")

	// The wizard polls for the one thing only the operator can do, and the
	// answer is the open step and nothing else.
	var step struct {
		Step int `json:"step"`
	}
	if err := json.Unmarshal([]byte(fetch(t, client, ts.URL+"/api/instalacao")), &step); err != nil {
		t.Fatal(err)
	}
	if step.Step != 1 {
		t.Fatalf("the poller reported step %d while the licence was missing", step.Step)
	}

	// Clicking the link in the inbox is what moves it on: the callback comes
	// back to the wizard, which is now asking for the WhatsApp account.
	r, err = client.Get(ts.URL + "/instancias/licenca/retorno?code=one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("the activation link landed on %s", r.Request.URL.Path)
	}
	mustContain(t, string(page), "wizard step 2", "Conectar o WhatsApp", `name="name"`, `value="instalacao"`)
}

// Each step of the wizard is the state of the gateway, not a stored notion of
// progress, and every form it borrows from the panel comes back to it.
func TestTheWizardWalksFromPairingToTheHandover(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{qr: evolution.QRCode{Image: "data:image/png;base64," + smallPNG}}
	ts, client := signedIn(t, repo, evo)

	// Creating the account from the wizard returns to the wizard, which is now
	// showing the QR code instead of sending the operator to another page.
	r, err := client.PostForm(ts.URL+"/instancias", url.Values{"name": {"pessoal"}, "origem": {"instalacao"}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("creating an instance from the wizard landed on %s", r.Request.URL.Path)
	}
	mustContain(t, string(page), "wizard pairing", "Dispositivos conectados", "data:image/png;base64,", `http-equiv="refresh"`)

	// Once the phone is paired the wizard is done and hands over to the panel,
	// which is where connecting a client already lives.
	evo.mu.Lock()
	evo.instances[0].Status = evolution.StatusConnected
	evo.mu.Unlock()
	done := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, done, "wizard done", "Tudo pronto", `href="/"`)
	mustNotContain(t, done, "wizard done", "Ativar a licença", "Dispositivos conectados")

	// And the panel takes over from there: nothing redirects back.
	r, err = client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Request.URL.Path != "/" {
		t.Fatalf("a finished installation was sent back to %s", r.Request.URL.Path)
	}
}

// A deployment that has already issued a key is past its installation, so a
// later outage belongs on the panel — where the state page and the instance
// controls are — and not on a wizard that cannot help with it.
func TestALaterOutageDoesNotReopenTheWizard(t *testing.T) {
	repo := newRepo()
	if err := repo.SaveInstance(context.Background(), "one", "Pessoal", "tok"); err != nil {
		t.Fatal(err)
	}
	repo.selected = "one"
	evo := &fakeEvolution{err: errors.New("connection refused")}
	ts, client := signedIn(t, repo, evo)
	if _, err := client.PostForm(ts.URL+"/chaves", url.Values{"name": {"notebook"}}); err != nil {
		t.Fatal(err)
	}

	r, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.Request.URL.Path != "/" {
		t.Fatalf("an outage on a working deployment landed on %s", r.Request.URL.Path)
	}
}

// smallPNG is a one-pixel image, which is all the QR assertions need.
const smallPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// The address is collected once, on the form that creates the administrator,
// and the wizard spends it: the first sign-in lands on an inbox to check
// rather than on a second form asking for the same thing.
func TestTheFirstSignInAlreadyHasTheActivationLinkOnItsWay(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
	}
	ts := httptest.NewServer(NewWebHandler(repo, evo, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	r, err := client.PostForm(ts.URL+"/setup", url.Values{
		"email": {"bruno@example.com"}, "password": {"senha segura 123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("creating the administrator ended at %s", r.Request.URL.Path)
	}
	if !evo.did("register-operator") {
		t.Fatal("the wizard asked for an address it had already been given")
	}
	mustContain(t, string(page), "wizard after setup", "Enviamos um link de ativação", "bruno@example.com", "Verificando a ativação")

	// And exactly one link goes out however many times the page is rendered:
	// the wait is recorded, so a refresh is a refresh and not another email.
	fetch(t, client, ts.URL+"/instalacao")
	fetch(t, client, ts.URL+"/instalacao")
	sent := 0
	evo.mu.Lock()
	for _, call := range evo.calls {
		if call == "register-operator" {
			sent++
		}
	}
	evo.mu.Unlock()
	if sent != 1 {
		t.Fatalf("%d activation links were sent for one wait", sent)
	}
}

// Evolution is usually still booting when the administrator is created, so the
// link cannot go out yet. The wizard sends it on the first render that finds
// Evolution awake, and asks for nothing in the meantime beyond a button.
func TestTheLinkGoesOutOnceEvolutionIsAwake(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{err: errors.New("connection refused"), registerErr: errors.New("connection refused")}
	ts := httptest.NewServer(NewWebHandler(repo, evo, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.PostForm(ts.URL+"/setup", url.Values{
		"email": {"bruno@example.com"}, "password": {"senha segura 123"},
	}); err != nil {
		t.Fatal(err)
	}

	// Evolution comes up, without a licence.
	evo.mu.Lock()
	evo.err, evo.registerErr = evolution.ErrNotActivated, nil
	evo.license = evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"}
	evo.mu.Unlock()

	page := fetch(t, client, ts.URL+"/instalacao")
	if !evo.did("register-operator") {
		t.Fatal("the link was never sent although Evolution was up and unlicensed")
	}
	mustContain(t, page, "wizard once awake", "bruno@example.com", "Verificando a ativação")
	if repo.license.LinkSentAt.IsZero() {
		t.Fatal("the wait was not recorded, so a refresh would send another link")
	}
}

// The length rule is a boundary, so it is worth pinning: six is a password the
// panel accepts, five is not, and the server decides either way — the form's
// own minlength is a convenience the browser can be talked out of.
func TestPasswordLengthIsEnforcedByTheServer(t *testing.T) {
	short := httptest.NewServer(NewWebHandler(newRepo(), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer short.Close()
	r, err := http.PostForm(short.URL+"/setup", url.Values{"email": {"bruno@example.com"}, "password": {"cinco"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "setup", "pelo menos 6 caracteres")

	repo := newRepo()
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if _, err := client.PostForm(ts.URL+"/setup", url.Values{"email": {"bruno@example.com"}, "password": {"seisok"}}); err != nil {
		t.Fatal(err)
	}
	if repo.user != "bruno@example.com" {
		t.Fatalf("a six-character password was refused: administrator = %q", repo.user)
	}
}

// A failed activation has to be readable by whoever clicked, which is the
// whole point of the click landing on a page of its own. Sending them to a
// page that needs a session threw the reason away on the redirect and left the
// wizard waiting on a licence that had already been refused.
func TestAFailedActivationSaysWhyWhereverItWasClicked(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:           evolution.ErrNotActivated,
		license:       evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
		activationErr: errors.New("licensing server returned HTTP 400"),
	}
	ts, client := signedIn(t, repo, evo)

	// From the mail client's own browser: no session, and it still answers.
	bare := &http.Client{}
	r, err := bare.Get(ts.URL + "/instancias/licenca/retorno?code=spent")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.Request.URL.Path != "/instancias/licenca/retorno" {
		t.Fatalf("a session-less failure was sent to %s", r.Request.URL.Path)
	}
	mustContain(t, string(body), "callback failure", "licensing server returned HTTP 400", "Não recebeu o e-mail?")
	mustNotContain(t, string(body), "callback failure", `name="password"`)

	// And from the browser that is already signed in, the same reason lands on
	// the wizard, where the next attempt is.
	page := fetch(t, client, ts.URL+"/instancias/licenca/retorno?code=spent")
	mustContain(t, page, "wizard failure", "licensing server returned HTTP 400", "Ativar a licença")
}

// The cookie has to survive the return from the licensing server, which is a
// cross-site navigation. Strict drops it there, and the operator arrives at
// their own panel signed in and is shown the login form.
func TestTheSessionCookieSurvivesAnExternalReturn(t *testing.T) {
	repo := newRepo()
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
	defer ts.Close()
	// The redirect is not followed, because the cookie is set on the response
	// that issues it and the jar would swallow it.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	r, err := client.PostForm(ts.URL+"/setup", url.Values{"email": {"bruno@example.com"}, "password": {"senha segura 123"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	for _, cookie := range r.Cookies() {
		if cookie.Name != "whatsapp_mcp_session" {
			continue
		}
		if cookie.SameSite == http.SameSiteStrictMode {
			t.Fatal("the session cookie is Strict, so it is not sent when the licensing server sends the operator back")
		}
		if !cookie.HttpOnly {
			t.Fatal("the session cookie is readable from scripting")
		}
		return
	}
	t.Fatal("no session cookie was issued")
}

// In automatic mode nobody types an address: the wizard registers the licence
// with an address of its own on the first render that can, and every later
// render is only polling on the worker's click coming back.
func TestAutomaticLicenceRegistrationRunsItself(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
	}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()

	page := fetch(t, client, ts.URL+"/instalacao")
	if !evo.did("register-operator") {
		t.Fatal("the automatic registration never went out")
	}
	address := evo.operatorEmail
	if !strings.HasPrefix(address, "whatsappmcp+") || !strings.HasSuffix(address, "@brorlandi.xyz") {
		t.Fatalf("registration address = %q, want whatsappmcp+…@brorlandi.xyz", address)
	}
	mustContain(t, page, "wizard automatic", "Verificando a licença do software", "Ativando…")
	if strings.Contains(page, htmlEscaped(address)) || strings.Contains(page, "whatsappmcp") {
		t.Fatalf("the wizard showed the internal licence address:\n%s", page)
	}
	if !strings.Contains(page, `data-onboarding="1"`) {
		t.Fatal("the wizard did not keep polling for the licence to come in")
	}

	// Polling must not mint a second registration: the same address is the
	// wait's identity until the click answers it.
	second := fetch(t, client, ts.URL+"/instalacao")
	if evo.operatorEmail != address {
		t.Fatalf("a poll re-registered as %q, want the same %q", evo.operatorEmail, address)
	}
	mustContain(t, second, "wizard automatic", "Verificando a licença do software")
	if strings.Contains(second, htmlEscaped(address)) {
		t.Fatal("a poll leaked the internal licence address")
	}
}

// The retry button goes back to the wizard it came from, and a failure to
// reach the licensing server says so rather than looking like a lost click.
func TestAutomaticLicenceRetryReturnsToItsWizard(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, registerErr: errors.New("licensing server down")}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()
	evo.mu.Lock()
	evo.registerErr = nil
	evo.mu.Unlock()

	r, err := client.PostForm(ts.URL+"/instancias/licenca/auto", url.Values{"origem": {"instalacao"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	mustContain(t, string(body), "wizard automatic", "Verificando a licença do software")
	if strings.Contains(string(body), "whatsappmcp") {
		t.Fatal("the retry leaked the internal licence address")
	}
	if r.Request.URL.Path != "/instalacao" {
		t.Fatalf("retry landed on %s", r.Request.URL.Path)
	}
	if !evo.did("register-operator") {
		t.Fatal("the button did not ask for a registration")
	}
}

// Signing in through the setup form is what fills the operator's address, and
// automatic mode must not sit on it: the licence goes to the worker's address
// even when a personal one is on file.
func TestAutomaticModeDoesNotWaitOnTheOperatorInbox(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, license: evolution.License{Status: "inactive"}}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()
	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "wizard automatic", "Verificando a licença do software")
	// The registration still has to go to the worker's address rather than the
	// operator's; it just must not be named on the page.
	if !strings.HasPrefix(evo.operatorEmail, "whatsappmcp+") {
		t.Fatalf("the wait was put on %q, want the worker's own address", evo.operatorEmail)
	}
	if strings.Contains(page, "Enviamos um link de ativação para") {
		t.Fatal("the wizard told the operator to open an inbox in automatic mode")
	}
}

// htmlEscaped renders what the address looks like inside a rendered page: the
// plus-addressing character is one html/template escapes, so a test looking for
// the raw form would miss a licence address that is right there.
func htmlEscaped(value string) string {
	return strings.ReplaceAll(value, "+", "&#43;")
}

// A worker that never clicks is the failure automatic mode has to survive. The
// wizard cannot keep promising a licence is on its way, and the answer it owes
// the operator is the one flow that needs nothing of this project's own
// infrastructure: their inbox, their click.
func TestAutomaticLicenceFallsBackToTheOperatorInbox(t *testing.T) {
	shortAutoWait(t)
	repo := newRepo()
	repo.mu.Lock()
	repo.license = store.EvolutionLicense{OperatorEmail: "whatsappmcp+abc@brorlandi.xyz", LinkSentAt: time.Now().Add(-time.Hour)}
	repo.mu.Unlock()
	evo := &fakeEvolution{
		err:     evolution.ErrNotActivated,
		license: evolution.License{Status: "inactive", RegisterURL: "https://license.example/register?token=abc"},
	}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()
	repo.mu.Lock()
	repo.user = "admin@example.com"
	repo.mu.Unlock()

	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "wizard stalled", "não se completou", "E-mail para a licença", `name="email"`)
	if strings.Contains(page, "Verificando a ativação") {
		t.Fatal("the wizard was still promising an activation that is not coming")
	}
	// The licence address and the sign-in address are separate things: the
	// licensing server registers one licence per address, ever, so offering
	// the address the operator signs in with would be advice that fails for
	// anyone who has licensed an installation before. The field starts empty.
	if strings.Contains(page, `value="admin@example.com"`) {
		t.Fatal("the fallback offered the sign-in address as the licence address")
	}
	// A late click still has to land, so the poll stays armed.
	if !strings.Contains(page, `data-onboarding="1"`) {
		t.Fatal("the wizard stopped watching for the licence")
	}

	// Taking over moves the wait to their inbox, and the wizard's copy follows
	// it there even though automatic mode is still switched on.
	r, err := client.PostForm(ts.URL+"/instancias/licenca", url.Values{"email": {"eu@example.com"}, "origem": {"instalacao"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if got := evo.operatorEmail; got != "eu@example.com" {
		t.Fatalf("the registration went to %q, want the operator's own address", got)
	}
	mustContain(t, string(body), "wizard manual", "eu@example.com", "Abra o e-mail e clique no link")
	if strings.Contains(string(body), "Verificando a licença do software") {
		t.Fatal("the wizard still described the automatic flow after the hand-over")
	}
}

// The other way automatic mode fails is never getting a registration out at
// all — Evolution refusing, or the licensing server refusing. It looks
// different from the inside and identical to the operator, so it ends in the
// same place.
func TestAutomaticLicenceThatNeverSendsFallsBackToo(t *testing.T) {
	shortAutoWait(t)
	repo := newRepo()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, registerErr: errors.New("licensing server down")}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()

	// The first render is the attempt that fails and starts the clock; the
	// next one is past the wait.
	fetch(t, client, ts.URL+"/instalacao")
	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "wizard stalled", "não se completou", "não chegou a sair", "E-mail para a licença")
	if strings.Contains(page, `http-equiv="refresh"`) {
		t.Fatal("the wizard kept reloading itself instead of handing over")
	}
}

// Inside the wait, nothing changes: the automatic path is given its chance
// before the operator is asked for anything.
func TestAutomaticLicenceWaitsBeforeHandingOver(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, license: evolution.License{Status: "inactive"}}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()
	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "wizard automatic", "Verificando a licença do software", "Ativando…")
	if strings.Contains(page, "não se completou") {
		t.Fatal("the wizard gave up on the automatic path inside its own wait")
	}
}

// The automatic address lives on the maintainer's own domain. It is how the
// worker gets the mail and it is none of the operator's business: naming it
// on the page raises a question with no good answer. The leak this guards is
// the awkward one — a deployment that registered automatically and then had
// EVOLUTION_LICENSE_AUTO turned off, whose manual copy would name the address
// it was waiting on.
func TestManualModeNeverNamesTheAutomaticAddress(t *testing.T) {
	repo := newRepo()
	repo.mu.Lock()
	repo.license = store.EvolutionLicense{
		OperatorEmail: "whatsappmcp+abc@brorlandi.xyz",
		LinkSentAt:    time.Now(),
	}
	repo.mu.Unlock()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, license: evolution.License{Status: "inactive"}}
	ts, client := signedIn(t, repo, evo, false)
	defer ts.Close()

	page := fetch(t, client, ts.URL+"/instalacao")
	if strings.Contains(page, "whatsappmcp") {
		t.Fatalf("the manual wizard named the automatic licence address:\n%s", page)
	}
	mustContain(t, page, "wizard manual", "link de ativação")
}

// Registering the licence by hand has to be reachable before the timeout too:
// an operator who would rather own the registration should not have to wait
// three minutes for a failure to be offered the choice. It is a disclosure,
// not the page's question — the automatic path is what the page is doing.
func TestManualRegistrationIsOfferedDuringTheAutomaticWait(t *testing.T) {
	repo := newRepo()
	evo := &fakeEvolution{err: evolution.ErrNotActivated, license: evolution.License{Status: "inactive"}}
	ts, client := signedIn(t, repo, evo, true)
	defer ts.Close()

	page := fetch(t, client, ts.URL+"/instalacao")
	mustContain(t, page, "wizard automatic", "Verificando a licença do software",
		"Prefiro ativar manualmente", `action="/instancias/licenca"`, `name="email"`)
	// Offered, not asked: the licence form's button is never the page's
	// primary action while the automatic path is still running.
	if strings.Contains(page, "btn btn--block") {
		t.Fatalf("the manual form was presented as the primary action:\n%s", page)
	}
	// And it is not framed as a missing email, because none was ever sent to
	// the operator in this mode.
	if strings.Contains(page, "Não recebeu o e-mail") {
		t.Fatal("the automatic wait asked about an email the operator never gets")
	}
}

// The tab bar marks the status page only when there is something to look at.
// A green dot that is always green stops being read within a day, and then the
// one day it changes nobody notices.
func TestTheStatusTabIsMarkedOnlyWhenSomethingIsWrong(t *testing.T) {
	for _, session := range []struct {
		state  string
		marked bool
		class  string
	}{
		{"connected", false, ""},
		{"pairing", true, `class="nav__alert nav__alert--warn"`},
		{"logged_out", true, `class="nav__alert"`},
	} {
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
		evo := &fakeEvolution{instances: []evolution.Instance{{ID: "one", Name: "Pessoal", Status: evolution.StatusConnected}}}

		state := health.NewState()
		state.SetDependencies(true, true, true)
		state.MarkEvent(time.Now())
		state.SetWhatsApp(session.state, "", "", "Bruno")

		ts := httptest.NewServer(NewWebHandler(repo, evo, state, testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil))
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		page := fetch(t, client, ts.URL+"/")
		ts.Close()

		if session.marked {
			mustContain(t, page, session.state, session.class, "Atenção: ")
		} else {
			mustNotContain(t, page, session.state, `class="nav__alert`, "Atenção: ")
		}
	}
}
