package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
)

type fakeRepo struct {
	mu                   sync.Mutex
	user, hash, selected string
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

type fakeEvolution struct {
	instances []evolution.Instance
	err       error
}

func testSessionKey() []byte { return []byte("test-only-session-key-that-is-long-enough") }

func (f fakeEvolution) FetchInstances(context.Context) ([]evolution.Instance, error) {
	return f.instances, f.err
}

func TestSetupCreatesOnlyOneAdminAndLoginWorks(t *testing.T) {
	repo := &fakeRepo{}
	ts := httptest.NewServer(NewWebHandler(repo, fakeEvolution{}, testSessionKey(), "http://evolution:4000"))
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
	page, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "http://evolution:4000/manager/login") {
		t.Fatalf("manager link missing: %s", body)
	}
}

// fetch GETs a page and returns its body. A nil client uses an anonymous one,
// which is what the unauthenticated setup and login pages need.
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

// The brand mark is inlined from internal/brand/logo.svg, so every page must
// carry the SVG itself instead of a link to an external asset.
func TestEveryPageInlinesTheBrandLogo(t *testing.T) {
	logo := string(brand.LogoSVG())
	if !strings.HasPrefix(logo, "<svg") {
		t.Fatalf("brand.LogoSVG is not inline SVG: %q", logo)
	}

	fresh := httptest.NewServer(NewWebHandler(&fakeRepo{}, fakeEvolution{}, testSessionKey(), "http://evolution:4000"))
	defer fresh.Close()
	setup := fetch(t, nil, fresh.URL+"/setup")
	mustContain(t, setup, "setup", logo, "Configuração inicial", `name="username"`, `name="password"`)

	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{user: "admin", hash: hash}
	ts := httptest.NewServer(NewWebHandler(repo, fakeEvolution{}, testSessionKey(), "http://evolution:4000"))
	defer ts.Close()
	mustContain(t, fetch(t, nil, ts.URL+"/login"), "login", logo, `name="username"`, `name="password"`)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	mustContain(t, fetch(t, client, ts.URL+"/"), "dashboard", logo, "http://evolution:4000/manager/login")
}

// The dashboard must always offer the public Evolution Manager button, and must
// stay readable when Evolution is down or has no instance yet.
func TestDashboardShowsManagerLinkAndInstanceStates(t *testing.T) {
	hash, err := auth.HashPassword("senha segura 123")
	if err != nil {
		t.Fatal(err)
	}
	login := func(evo fakeEvolution, selected string) string {
		t.Helper()
		ts := httptest.NewServer(NewWebHandler(&fakeRepo{user: "admin", hash: hash, selected: selected}, evo, testSessionKey(), "https://evolution.example"))
		t.Cleanup(ts.Close)
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		r, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return fetch(t, client, ts.URL+"/")
	}

	const manager = `href="https://evolution.example/manager/login"`

	empty := login(fakeEvolution{}, "")
	mustContain(t, empty, "empty dashboard", manager, "Nenhuma instância encontrada")

	down := login(fakeEvolution{err: errors.New("boom")}, "")
	mustContain(t, down, "unavailable dashboard", manager, `role="alert"`, "A API Evolution está indisponível no momento.")
	if strings.Contains(down, `name="instance_id"`) {
		t.Error("unavailable dashboard must not offer a selection form")
	}

	instances := []evolution.Instance{
		{ID: "one", Name: "Suporte", Number: "5511999999999", Status: evolution.StatusConnected},
		{ID: "two", Name: "Vendas", Status: evolution.StatusConnecting},
		{ID: "three", Name: "Antiga", Status: evolution.StatusDisconnected},
	}
	listed := login(fakeEvolution{instances: instances}, "one")
	mustContain(t, listed, "populated dashboard", manager,
		"Conectada", "Conectando", "Desconectada", "Em uso pelo MCP",
		`value="one" checked`, "Remover seleção", "5511999999999")
}

func TestSelectionAllowsOnlyListedSingleInstance(t *testing.T) {
	repo := &fakeRepo{}
	instances := []evolution.Instance{{ID: "one", Name: "Um", Status: evolution.StatusConnected}, {ID: "two", Name: "Dois", Status: evolution.StatusDisconnected}}
	ts := httptest.NewServer(NewWebHandler(repo, fakeEvolution{instances: instances}, testSessionKey(), "http://evolution:4000"))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, _ := client.PostForm(ts.URL+"/setup", url.Values{"username": {"admin"}, "password": {"senha segura 123"}})
	r.Body.Close()
	r, _ = client.PostForm(ts.URL+"/selection", url.Values{"instance_id": {"one"}, "instance_id_extra": {"two"}})
	r.Body.Close()
	if repo.selected != "one" {
		t.Fatalf("selected=%q", repo.selected)
	}
	r, _ = client.PostForm(ts.URL+"/selection", url.Values{"instance_id": {"unknown"}})
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
