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
