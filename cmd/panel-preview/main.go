// Command panel-preview serves the control panel against fabricated data.
//
// It exists so the panel's layout and wording can be worked on without a
// database, a queue or a paired WhatsApp account — the parts of the stack that
// make a UI change slow to see. It talks to nothing and stores nothing.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/httpapi"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

type fakeStore struct {
	mu   sync.Mutex
	hash string
	keys []store.APIKey
	next int64
	// license is what the panel was handed by a completed registration, kept
	// so the activation flow can be clicked through to its end.
	license store.EvolutionLicense
	// used mirrors a client having authenticated at least once, which is what
	// closes the second step. PREVIEW_CONNECTED=true starts in that state.
	used bool
	// user is empty on a deployment nobody has set up yet, which is where the
	// first-run form lives. PREVIEW_SETUP=true starts there, so the whole
	// installation can be walked from the very first screen.
	user string
}

func (f *fakeStore) Admin(context.Context) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user == "" {
		return "", "", httpapi.ErrNotFound
	}
	return f.user, f.hash, nil
}
func (f *fakeStore) CreateAdmin(_ context.Context, user, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user != "" {
		return httpapi.ErrAdminExists
	}
	f.user, f.hash = user, hash
	return nil
}
func (f *fakeStore) AdminMustChangePassword(context.Context) (bool, error) { return false, nil }
func (f *fakeStore) SetAdminPassword(context.Context, string) error        { return nil }
func (f *fakeStore) SelectedInstance(context.Context) (string, error)      { return "inst-1", nil }
func (f *fakeStore) SelectInstance(context.Context, string) error          { return nil }
func (f *fakeStore) SaveInstance(context.Context, string, string, string) error {
	return nil
}
func (f *fakeStore) InstanceToken(context.Context, string) (string, error) { return "tok", nil }
func (f *fakeStore) ForgetInstance(context.Context, string) error          { return nil }
func (f *fakeStore) ManagedInstances(context.Context) (map[string]string, error) {
	return map[string]string{"inst-1": "pessoal"}, nil
}
func (f *fakeStore) Coverage(context.Context, string) (store.Coverage, error) {
	return store.Coverage{Messages: 18432, OldestAt: time.Now().Add(-62 * 24 * time.Hour), NewestAt: time.Now().Add(-3 * time.Minute)}, nil
}
func (f *fakeStore) CreateAPIKey(_ context.Context, name, instance, digest, prefix string) error {
	f.next++
	key := store.APIKey{ID: f.next, Name: name, InstanceID: instance, Prefix: prefix, CreatedAt: time.Now()}
	if f.used {
		// A used credential is one whose client has shaken hands, so it also
		// carries the name that client gave itself — which is what the panel
		// shows in place of the label typed here.
		key.LastUsedAt = time.Now().Add(-90 * time.Minute)
		key.ClientName, key.ClientVersion = "claude-ai", "0.14.2"
		if f.next%2 == 0 {
			key.ClientName, key.ClientVersion = "claude-code", "2.1.0"
		}
	}
	f.keys = append(f.keys, key)
	return nil
}
func (f *fakeStore) ListAPIKeys(context.Context) ([]store.APIKey, error) { return f.keys, nil }
func (f *fakeStore) RevokeAPIKey(context.Context, int64) error           { return nil }
func (f *fakeStore) OldestMessage(context.Context, string, string) (store.Message, error) {
	return store.Message{MessageID: "OLD", ChatJID: "a@s.whatsapp.net", SentAt: time.Now().Add(-62 * 24 * time.Hour)}, nil
}
func (f *fakeStore) SaveOperatorEmail(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.license.OperatorEmail = email
	return nil
}
func (f *fakeStore) MarkLicenseLinkSent(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.license.OperatorEmail, f.license.LinkSentAt = email, time.Now()
	return nil
}
func (f *fakeStore) SaveEvolutionLicense(_ context.Context, license store.EvolutionLicense) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	email := f.license.OperatorEmail
	f.license = license
	f.license.OperatorEmail = email
	return nil
}
func (f *fakeStore) EvolutionLicense(context.Context) (store.EvolutionLicense, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.license.APIKey == "" && f.license.OperatorEmail == "" {
		return store.EvolutionLicense{}, store.ErrNoLicense
	}
	return f.license, nil
}

// unlicensed is the state an installation is in before anyone registers a
// licence: Evolution answers 503 on every route, and the panel shows the
// activation card instead of the instance list. PREVIEW_UNLICENSED=true starts
// there, because that card is otherwise the one screen impossible to look at
// without tearing down a real Evolution.
type fakeEvo struct {
	mu         sync.Mutex
	connected  bool
	unlicensed bool
}

// activated is how the preview leaves the unlicensed state: the same way a
// real Evolution does, by being handed a credential.
func (f *fakeEvo) activated() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlicensed = false
}

func (f *fakeEvo) needsLicense() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unlicensed
}

func (f *fakeEvo) FetchInstances(context.Context) ([]evolution.Instance, error) {
	if f.needsLicense() {
		return nil, evolution.ErrNotActivated
	}
	status := evolution.StatusConnected
	if !f.connected {
		status = evolution.StatusDisconnected
	}
	return []evolution.Instance{
		{ID: "inst-1", Name: "pessoal", Number: "5511923456789:89", Status: status, Token: "tok"},
		{ID: "inst-2", Name: "trabalho", Number: "5511988887777", Status: evolution.StatusDisconnected},
	}, nil
}
func (f *fakeEvo) CreateInstance(_ context.Context, name, token string) (evolution.Instance, error) {
	return evolution.Instance{ID: "new", Name: name}, nil
}
func (f *fakeEvo) DeleteInstance(context.Context, string) error     { return nil }
func (f *fakeEvo) ConnectInstance(context.Context, string) error    { return nil }
func (f *fakeEvo) DisconnectInstance(context.Context, string) error { return nil }
func (f *fakeEvo) LogoutInstance(context.Context, string) error     { return nil }
func (f *fakeEvo) QRCode(context.Context, string) (evolution.QRCode, error) {
	return evolution.QRCode{Image: "data:image/png;base64," + sampleQR, Code: "2@AbCdEf"}, nil
}
func (f *fakeEvo) RequestHistory(context.Context, string, evolution.Anchor, int) error { return nil }
func (f *fakeEvo) License(context.Context, string) (evolution.License, error) {
	if f.needsLicense() {
		return evolution.License{
			Status:      "inactive",
			InstanceID:  "53045908-0603-4001-8f59-505c1a00323a",
			RegisterURL: "https://license.example/register?token=preview",
		}, nil
	}
	return evolution.License{Status: "active"}, nil
}

// RegisterOperator stands in for the licensing server sending a magic link.
// There is no inbox here, so the link it would have emailed is printed to the
// terminal — that is the only step of this flow a preview cannot perform.
func (f *fakeEvo) RegisterOperator(_ context.Context, email, name, callback string) error {
	log.Printf("registro de licença para %q em nome de %q", email, name)
	log.Printf("o link que chegaria por e-mail: %s?code=preview-authorization-code", callback)
	return nil
}
func (f *fakeEvo) CompleteActivation(context.Context, string) (evolution.LicenseActivation, error) {
	f.activated()
	return evolution.LicenseActivation{
		APIKey:     "evo-preview-key",
		Tier:       "evolution-go",
		CustomerID: 42,
		InstanceID: "53045908-0603-4001-8f59-505c1a00323a",
	}, nil
}
func (f *fakeEvo) ReactivateLicense(context.Context, string) error {
	f.activated()
	return nil
}

const (
	previewUser     = "admin@exemplo.com"
	previewPassword = "senha segura 123"
)

func main() {
	hash, _ := auth.HashPassword(previewPassword)
	state := health.NewState()
	state.SetDependencies(true, true, true)
	state.MarkEvent(time.Now().Add(-40 * time.Second))
	state.SetWhatsApp("connected", "", "5511999999999@s.whatsapp.net", "Fulano de Tal")
	for _, q := range []string{"message", "sendmessage", "historysync", "connected", "disconnected", "loggedout", "pairsuccess", "connectfailure", "temporaryban"} {
		state.SetQueueConsuming(q, true, "")
		state.MarkQueueEvent(q, time.Now().Add(-2*time.Minute), false)
	}
	unlicensed := os.Getenv("PREVIEW_UNLICENSED") == "true"
	st := &fakeStore{hash: hash, user: previewUser, used: os.Getenv("PREVIEW_CONNECTED") == "true"}
	if os.Getenv("PREVIEW_SETUP") == "true" {
		st.user, st.hash = "", ""
	}
	// A seeded key is a deployment that has already worked once, which is what
	// keeps the panel from handing itself over to the installation wizard. The
	// unlicensed preview is the first run, so it starts without one.
	if !unlicensed {
		_ = st.CreateAPIKey(context.Background(), "Claude Desktop", "inst-1", "d", "wamcp-a1B2c3")
		_ = st.CreateAPIKey(context.Background(), "Claude Code", "inst-1", "e", "wamcp-Z9y8X7")
	}
	// The public address is what the panel puts into MCP snippets and into the
	// licence callback. A made-up domain reads better in screenshots; pointing
	// it at this server is what makes the licence link clickable here.
	// PREVIEW_LICENSE_AUTO_WAIT=1s walks straight into the stalled state,
	// which is otherwise a three-minute wait to look at.
	autoWait := 3 * time.Minute
	if raw := os.Getenv("PREVIEW_LICENSE_AUTO_WAIT"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			autoWait = parsed
		}
	}
	// The version in the footer and the "newer release available" banner are
	// panel states like any other, so they can be worked on here rather than
	// only on a server that happens to be behind. PREVIEW_VERSION is what this
	// build claims to be; PREVIEW_LATEST is what the update check found.
	if stamped := os.Getenv("PREVIEW_VERSION"); stamped != "" {
		version.Version = stamped
	}
	version.Record(os.Getenv("PREVIEW_LATEST"))

	publicURL := os.Getenv("PREVIEW_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "https://whatsapp-mcp.example.com"
	}
	// The preview defaults to the automatic licence path, which is what a real
	// install shows; PREVIEW_LICENSE_AUTO=false previews the manual one.
	handler := httpapi.NewWebHandler(st, &fakeEvo{connected: os.Getenv("PREVIEW_PAIRED") != "false", unlicensed: unlicensed}, state, []byte("preview-session-key-preview-session-key"), publicURL, "", os.Getenv("PREVIEW_LICENSE_AUTO") != "false", "brorlandi.xyz", autoWait)
	// A second preview on the same machine would otherwise fail to bind and
	// die silently, which reads as the panel being broken.
	address := os.Getenv("PREVIEW_ADDR")
	if address == "" {
		address = "127.0.0.1:8090"
	}
	if st.user == "" {
		log.Printf("painel de demonstração em http://%s — comece por /setup, criando o administrador", address)
	} else {
		log.Printf("painel de demonstração em http://%s — usuário %q, senha %q", address, previewUser, previewPassword)
	}
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatal(err)
	}
}
