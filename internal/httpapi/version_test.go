package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

// The panel says what it is running, because an operator asked to update needs
// to know whether they already did.
func TestFooterCarriesTheVersion(t *testing.T) {
	defer stampVersion(t, "v0.1.0-beta")()
	ts, client := signedIn(t, newRepo(), &fakeEvolution{})
	mustContain(t, fetch(t, client, ts.URL+"/"), "conectar", "v0.1.0-beta")
}

func TestUpdateBanner(t *testing.T) {
	t.Run("appears on a signed-in page when a newer release exists", func(t *testing.T) {
		defer stampVersion(t, "v0.1.0-beta")()
		defer publishRelease(t, "0.2.0")()
		ts, client := signedIn(t, newRepo(), &fakeEvolution{})
		page := fetch(t, client, ts.URL+"/documentacao")
		mustContain(t, page, "documentacao",
			"0.2.0",
			// The command is on the page, ready to copy: an operator who has
			// just been told to update should not have to go looking for how.
			updateCommand,
			"/releases/tag/v0.2.0")
	})

	// An unauthenticated visitor gets no banner. "This box is running an
	// outdated version" is an invitation, and the sign-in page is where
	// whoever found the URL is standing.
	t.Run("never on the sign-in page", func(t *testing.T) {
		defer stampVersion(t, "v0.1.0-beta")()
		defer publishRelease(t, "0.2.0")()
		ts := httptest.NewServer(NewWebHandler(signedUpRepo(t), &fakeEvolution{}, health.NewState(), testSessionKey(), "https://mcp.example", "", false, "brorlandi.xyz", 3*time.Minute))
		defer ts.Close()
		mustNotContain(t, fetch(t, nil, ts.URL+"/login"), "login", "0.2.0", updateCommand)
	})

	t.Run("stays quiet when the instance is current", func(t *testing.T) {
		defer stampVersion(t, "v0.2.0")()
		defer publishRelease(t, "0.2.0")()
		ts, client := signedIn(t, newRepo(), &fakeEvolution{})
		mustNotContain(t, fetch(t, client, ts.URL+"/documentacao"), "documentacao", updateCommand)
	})

	// Nothing has been checked yet on a panel that has just started, and on
	// one whose operator turned the check off it never will be.
	t.Run("stays quiet when no check has answered", func(t *testing.T) {
		defer stampVersion(t, "v0.1.0-beta")()
		defer publishRelease(t, "")()
		ts, client := signedIn(t, newRepo(), &fakeEvolution{})
		mustNotContain(t, fetch(t, client, ts.URL+"/documentacao"), "documentacao", updateCommand)
	})
}

// The probes answer what is running without a session, so a monitor can watch
// a fleet without holding a panel password.
func TestHealthCarriesTheVersion(t *testing.T) {
	defer stampVersion(t, "v0.1.0-beta")()
	ts := httptest.NewServer(Handler(health.NewState(), time.Minute))
	defer ts.Close()
	for _, probe := range []string{"/healthz", "/readyz"} {
		body := fetch(t, &http.Client{}, ts.URL+probe)
		if !strings.Contains(body, `"version":"0.1.0-beta"`) {
			t.Errorf("%s does not report the version: %s", probe, body)
		}
	}
}

func stampVersion(t *testing.T, v string) func() {
	t.Helper()
	previous := version.Version
	version.Version = v
	return func() { version.Version = previous }
}

func publishRelease(t *testing.T, tag string) func() {
	t.Helper()
	version.Record(tag)
	return func() { version.Record("") }
}

// signedUpRepo is an installation with an administrator and nobody signed in,
// which is what puts a visitor on the sign-in page rather than on the wizard.
func signedUpRepo(t *testing.T) *fakeRepo {
	t.Helper()
	repo := newRepo()
	repo.user, repo.hash = "admin", "$2a$10$notarealhashbutlongenoughtoparse"
	return repo
}
