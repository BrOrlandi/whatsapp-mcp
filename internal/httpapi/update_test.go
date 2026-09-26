package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/selfupdate"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

func updatePanel(t *testing.T, updater SelfUpdater) (*httptest.Server, *http.Client) {
	t.Helper()
	repo := newRepo()
	hash, _ := auth.HashPassword("senha segura 123")
	repo.user, repo.hash = "admin", hash
	ts := httptest.NewServer(NewWebHandler(repo, &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute, nil, WithSelfUpdate(updater)))
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

// With an agent on the host the banner offers a button, the button asks for
// exactly the advertised version, and the progress page takes over.
func TestUpdateButtonRequestsTheAdvertisedVersion(t *testing.T) {
	previous := version.Version
	t.Cleanup(func() { version.Version = previous; version.Record("") })
	version.Version = "v0.3.0-beta.2"
	version.Record("0.3.0-beta.3")

	dir := t.TempDir()
	updater := selfupdate.New(dir)
	ts, client := updatePanel(t, updater)

	page := fetch(t, client, ts.URL+"/estado")
	mustContain(t, page, "estado", "0.3.0-beta.3", "curl -fsSL")
	if strings.Contains(page, `action="/atualizar"`) {
		t.Fatal("button offered without an agent")
	}

	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"method":"install"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	page = fetch(t, client, ts.URL+"/estado")
	mustContain(t, page, "estado", `action="/atualizar"`, "Atualizar para a 0.3.0-beta.3")

	// A forged version is refused.
	page = postPage(t, client, ts.URL+"/atualizar", url.Values{"version": {"9.9.9"}})
	mustContain(t, page, "atualizacao", "Não há uma versão nova")
	if updater.Busy() {
		t.Fatal("a forged version was requested")
	}

	page = postPage(t, client, ts.URL+"/atualizar", url.Values{"version": {"0.3.0-beta.3"}})
	mustContain(t, page, "atualizacao", "Para a versão 0.3.0-beta.3", "Na fila", `data-target="0.3.0-beta.3"`)
	if !updater.Busy() {
		t.Fatal("no request was written")
	}
	mustContain(t, fetch(t, client, ts.URL+"/estado"), "estado", "Atualização em andamento")

	// The progress endpoint answers the version without a session.
	anonymous := fetch(t, nil, ts.URL+"/api/atualizacao")
	if !strings.Contains(anonymous, `"version":"0.3.0-beta.2"`) || strings.Contains(anonymous, "status") && !strings.Contains(anonymous, `"signed_in":false`) {
		t.Fatalf("anonymous: %s", anonymous)
	}
	if strings.Contains(anonymous, "queued") {
		t.Fatal("update status leaked without a session")
	}
	mustContain(t, fetch(t, client, ts.URL+"/api/atualizacao"), "api", `"state":"queued"`)
}
