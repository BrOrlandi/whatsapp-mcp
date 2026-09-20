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
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/httpapi"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type fakeStore struct {
	hash string
	keys []store.APIKey
	next int64
	// used mirrors a client having authenticated at least once, which is what
	// closes the second step. PREVIEW_CONNECTED=true starts in that state.
	used bool
}

func (f *fakeStore) Admin(context.Context) (string, string, error) {
	return previewUser, f.hash, nil
}
func (f *fakeStore) CreateAdmin(context.Context, string, string) error {
	return httpapi.ErrAdminExists
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
		key.LastUsedAt = time.Now().Add(-90 * time.Minute)
	}
	f.keys = append(f.keys, key)
	return nil
}
func (f *fakeStore) ListAPIKeys(context.Context) ([]store.APIKey, error) { return f.keys, nil }
func (f *fakeStore) RevokeAPIKey(context.Context, int64) error           { return nil }
func (f *fakeStore) OldestMessage(context.Context, string, string) (store.Message, error) {
	return store.Message{MessageID: "OLD", ChatJID: "a@s.whatsapp.net", SentAt: time.Now().Add(-62 * 24 * time.Hour)}, nil
}
func (f *fakeStore) SaveOperatorEmail(context.Context, string) error { return nil }
func (f *fakeStore) SaveEvolutionLicense(context.Context, store.EvolutionLicense) error {
	return nil
}
func (f *fakeStore) EvolutionLicense(context.Context) (store.EvolutionLicense, error) {
	return store.EvolutionLicense{}, store.ErrNoLicense
}

type fakeEvo struct{ connected bool }

func (f *fakeEvo) FetchInstances(context.Context) ([]evolution.Instance, error) {
	status := evolution.StatusConnected
	if !f.connected {
		status = evolution.StatusDisconnected
	}
	return []evolution.Instance{
		{ID: "inst-1", Name: "pessoal", Number: "5511999999999", Status: status, Token: "tok"},
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
	return evolution.License{Status: "active"}, nil
}
func (f *fakeEvo) RegisterOperator(context.Context, string, string, string) error { return nil }
func (f *fakeEvo) CompleteActivation(context.Context, string) (evolution.LicenseActivation, error) {
	return evolution.LicenseActivation{APIKey: "k", Tier: "evolution-go"}, nil
}
func (f *fakeEvo) ReactivateLicense(context.Context, string) error { return nil }

const (
	previewUser     = "admin"
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
	st := &fakeStore{hash: hash, used: os.Getenv("PREVIEW_CONNECTED") == "true"}
	_ = st.CreateAPIKey(context.Background(), "Claude Code no notebook", "inst-1", "d", "wamcp-a1B2c3")
	handler := httpapi.NewWebHandler(st, &fakeEvo{connected: true}, state, []byte("preview-session-key-preview-session-key"), "https://whatsapp-mcp.example.com", "")
	log.Printf("painel de demonstração em http://127.0.0.1:8090 — usuário %q, senha %q", previewUser, previewPassword)
	_ = http.ListenAndServe("127.0.0.1:8090", handler)
}
