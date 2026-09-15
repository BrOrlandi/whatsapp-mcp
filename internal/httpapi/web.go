package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// StatusReader exposes the live gateway state to the panel. It is the same
// snapshot the health endpoints and the MCP tools read, so all three describe a
// failure the same way.
type StatusReader interface {
	Snapshot() health.Snapshot
}

var ErrNotFound = errors.New("not found")
var ErrAdminExists = errors.New("admin already exists")

type ControlStore interface {
	Admin(context.Context) (string, string, error)
	CreateAdmin(context.Context, string, string) error
	SelectedInstance(context.Context) (string, error)
	SelectInstance(context.Context, string) error
	Coverage(context.Context, string) (store.Coverage, error)
	CreateAPIKey(context.Context, string, string, string, string) error
	ListAPIKeys(context.Context) ([]store.APIKey, error)
	RevokeAPIKey(context.Context, int64) error
	OldestMessage(context.Context, string, string) (store.Message, error)
	SaveInstance(context.Context, string, string, string) error
	InstanceToken(context.Context, string) (string, error)
	ForgetInstance(context.Context, string) error
	ManagedInstances(context.Context) (map[string]string, error)
}

// EvolutionAPI is the slice of Evolution this panel drives. The panel owns the
// whole instance lifecycle so the operator never needs the Evolution Manager,
// which is why creation, pairing and teardown all appear here.
type EvolutionAPI interface {
	FetchInstances(context.Context) ([]evolution.Instance, error)
	CreateInstance(context.Context, string, string) (evolution.Instance, error)
	DeleteInstance(context.Context, string) error
	ConnectInstance(context.Context, string) error
	DisconnectInstance(context.Context, string) error
	LogoutInstance(context.Context, string) error
	QRCode(context.Context, string) (evolution.QRCode, error)
	RequestHistory(context.Context, string, evolution.Anchor, int) error
}

type sessions struct {
	mu     sync.RWMutex
	key    []byte
	values map[string]time.Time
}

func newSessions(key []byte) *sessions {
	return &sessions{key: append([]byte(nil), key...), values: map[string]time.Time{}}
}
func (s *sessions) create() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	s.values[id] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return id + "." + s.sign(id)
}
func (s *sessions) sign(id string) string {
	m := hmac.New(sha256.New, s.key)
	_, _ = m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *sessions) valid(v string) bool {
	id, sig, ok := strings.Cut(v, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(id))) {
		return false
	}
	s.mu.RLock()
	expiry, ok := s.values[id]
	s.mu.RUnlock()
	return ok && time.Now().Before(expiry)
}
func (s *sessions) remove(v string) {
	id, _, _ := strings.Cut(v, ".")
	s.mu.Lock()
	delete(s.values, id)
	s.mu.Unlock()
}

func NewSessionKey() ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	return key, err
}

type webApp struct {
	store     ControlStore
	evolution EvolutionAPI
	status    StatusReader
	publicURL string
	sessions  *sessions
	templates *template.Template
}

func NewWebHandler(store ControlStore, client EvolutionAPI, status StatusReader, sessionKey []byte, publicURL string) http.Handler {
	a := &webApp{store: store, evolution: client, status: status, publicURL: strings.TrimRight(publicURL, "/"), sessions: newSessions(sessionKey), templates: template.Must(template.New("pages").Funcs(templateFuncs).Parse(pages))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.dashboard)
	mux.HandleFunc("GET /setup", a.setupPage)
	mux.HandleFunc("POST /setup", a.setup)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("POST /selection", a.selectInstance)
	mux.HandleFunc("POST /instances", a.createInstance)
	mux.HandleFunc("GET /pair", a.pairPage)
	mux.HandleFunc("POST /instances/connect", a.connectInstance)
	mux.HandleFunc("POST /instances/disconnect", a.disconnectInstance)
	mux.HandleFunc("POST /instances/logout", a.logoutInstance)
	mux.HandleFunc("POST /instances/delete", a.deleteInstance)
	mux.HandleFunc("POST /instances/history", a.syncHistory)
	mux.HandleFunc("POST /keys", a.createKey)
	mux.HandleFunc("POST /keys/revoke", a.revokeKey)
	mux.HandleFunc("GET /api/selected-instance", a.selectedJSON)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// data: is required for img-src because the pairing QR code arrives from
		// Evolution as an inline data URI; no other source is allowed.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// newInstanceToken mints the credential Evolution uses to resolve which
// instance a request targets. It is an internal secret, never shown in the UI.
func newInstanceToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func (a *webApp) admin(r *http.Request) (string, string, error) {
	u, h, err := a.store.Admin(r.Context())
	if err == nil && u == "" {
		return "", "", ErrNotFound
	}
	if err != nil && (errors.Is(err, ErrNotFound) || strings.Contains(strings.ToLower(err.Error()), "no rows")) {
		return "", "", ErrNotFound
	}
	return u, h, err
}
func (a *webApp) authenticated(r *http.Request) bool {
	c, err := r.Cookie("whatsapp_mcp_session")
	return err == nil && a.sessions.valid(c.Value)
}
func (a *webApp) require(w http.ResponseWriter, r *http.Request) bool {
	if a.authenticated(r) {
		return true
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return false
}

// render builds the page in memory before writing it. Rendering straight to the
// response would emit a half-built page followed by an error banner whenever
// anything failed mid-template, and would turn a client that hung up into a
// bogus 500 written over a response already in flight.
func (a *webApp) render(w http.ResponseWriter, name string, data any) {
	var page bytes.Buffer
	if err := a.templates.ExecuteTemplate(&page, name, data); err != nil {
		http.Error(w, "Erro ao renderizar página", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = page.WriteTo(w)
}
func (a *webApp) setupPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.admin(r); err == nil {
		http.Redirect(w, r, "/login", 303)
		return
	}
	a.render(w, "setup", nil)
}
func (a *webApp) setup(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.admin(r); err == nil {
		http.Error(w, "O administrador já existe.", 409)
		return
	}
	u := strings.TrimSpace(r.FormValue("username"))
	p := r.FormValue("password")
	if u == "" || len(p) < 10 || len([]byte(p)) > 72 {
		a.render(w, "setup", map[string]string{"Error": "Use um usuário e uma senha com pelo menos 10 caracteres."})
		return
	}
	h, err := auth.HashPassword(p)
	if err != nil {
		http.Error(w, "Erro interno", 500)
		return
	}
	if err = a.store.CreateAdmin(r.Context(), u, h); err != nil {
		http.Error(w, "O administrador já existe.", 409)
		return
	}
	a.setSession(w, r)
	http.Redirect(w, r, "/", 303)
}
func (a *webApp) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.admin(r); errors.Is(err, ErrNotFound) {
		http.Redirect(w, r, "/setup", 303)
		return
	}
	a.render(w, "login", nil)
}
func (a *webApp) login(w http.ResponseWriter, r *http.Request) {
	u, h, err := a.admin(r)
	if err != nil {
		http.Redirect(w, r, "/setup", 303)
		return
	}
	if !hmac.Equal([]byte(u), []byte(strings.TrimSpace(r.FormValue("username")))) || !auth.CheckPassword(h, r.FormValue("password")) {
		w.WriteHeader(401)
		a.render(w, "login", map[string]string{"Error": "Usuário ou senha inválidos."})
		return
	}
	a.setSession(w, r)
	http.Redirect(w, r, "/", 303)
}
func (a *webApp) setSession(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: "whatsapp_mcp_session", Value: a.sessions.create(), Path: "/", MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}
func (a *webApp) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("whatsapp_mcp_session"); err == nil {
		a.sessions.remove(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "whatsapp_mcp_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", 303)
}

// instanceView is one row of the dashboard list. Managed marks the instances
// this panel created, which are the only ones it holds a token for and
// therefore the only ones it can operate.
type instanceView struct {
	evolution.Instance
	Managed  bool
	Selected bool
}

type dashboardData struct {
	Selected, Notice, Error string
	Instances               []instanceView
	Unavailable             bool
	Ready                   bool
	NeedsPairing            bool
	SelectedName            string
	Status                  *statusView
	Keys                    []store.APIKey
	NewKey                  string
	Endpoint                string
}

// statusView is the operational picture the panel shows: what WhatsApp reports,
// what the ingestion is doing, and how much history is actually indexed.
type statusView struct {
	WhatsApp  health.WhatsApp
	Queues    []health.Queue
	Problems  []string
	Coverage  store.Coverage
	HasIndex  bool
	LastEvent time.Time
}

// statusView reads the live snapshot and pairs it with the index coverage. The
// coverage query can fail while the database is the thing that is broken, and
// that must not stop the rest of the status from rendering.
func (a *webApp) statusView(r *http.Request, selected string) *statusView {
	if a.status == nil {
		return nil
	}
	snapshot := a.status.Snapshot()
	view := &statusView{WhatsApp: snapshot.WhatsApp, Queues: snapshot.Queues, Problems: snapshot.Problems(), LastEvent: snapshot.LastEventAt}
	if selected != "" {
		if coverage, err := a.store.Coverage(r.Context(), selected); err == nil {
			view.Coverage, view.HasIndex = coverage, true
		}
	}
	return view
}

// dashboardState gathers everything the dashboard and its actions need: the
// live instance list from Evolution, the ids this panel manages, and which one
// is currently selected.
func (a *webApp) dashboardState(r *http.Request) dashboardData {
	selected, _ := a.store.SelectedInstance(r.Context())
	managed, _ := a.store.ManagedInstances(r.Context())
	instances, err := a.evolution.FetchInstances(r.Context())
	d := dashboardData{Selected: selected, Unavailable: err != nil, Status: a.statusView(r, selected), Endpoint: a.publicURL + "/mcp"}
	d.Keys, _ = a.store.ListAPIKeys(r.Context())
	if err != nil {
		d.Notice = "A API Evolution está indisponível no momento. Tente novamente em instantes."
		return d
	}
	for _, instance := range instances {
		_, isManaged := managed[instance.ID]
		view := instanceView{Instance: instance, Managed: isManaged, Selected: instance.ID == selected}
		if view.Selected {
			d.SelectedName = instance.Name
			switch {
			case instance.Status == evolution.StatusConnected:
				d.Ready = true
			case isManaged:
				d.NeedsPairing = true
			}
		}
		d.Instances = append(d.Instances, view)
	}
	switch {
	case len(d.Instances) == 0:
		d.Notice = "Nenhuma instância ainda. Crie a primeira abaixo e conecte o WhatsApp sem sair deste painel."
	case selected == "":
		d.Notice = "Escolha qual instância o MCP deve usar."
	case d.NeedsPairing:
		d.Notice = "A instância selecionada ainda não está conectada. Conecte o WhatsApp para liberar o MCP."
	case !d.Ready:
		d.Notice = "A instância selecionada não foi criada por este painel, então ele não pode operá-la. Crie uma instância aqui."
	}
	return d
}

func (a *webApp) dashboard(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	d := a.dashboardState(r)
	d.Error = r.URL.Query().Get("erro")
	a.render(w, "dashboard", d)
}

// selectedToken resolves the Evolution credential for the selected instance.
// An instance created outside this panel has no token here, and the panel says
// so instead of pretending the action is possible.
func (a *webApp) selectedToken(r *http.Request) (string, error) {
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", ErrNotFound
	}
	return a.store.InstanceToken(r.Context(), selected)
}

// fail sends the operator back to a page with a readable reason. Evolution
// error text can carry its own wording, so it is passed as a query value and
// escaped by the template, never interpolated into markup here.
func (a *webApp) fail(w http.ResponseWriter, r *http.Request, path, reason string) {
	http.Redirect(w, r, path+"?erro="+url.QueryEscape(reason), http.StatusSeeOther)
}

func (a *webApp) createInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 60 {
		a.fail(w, r, "/", "Informe um nome de instância com até 60 caracteres.")
		return
	}
	token, err := newInstanceToken()
	if err != nil {
		a.fail(w, r, "/", "Não foi possível gerar as credenciais da instância.")
		return
	}
	created, err := a.evolution.CreateInstance(r.Context(), name, token)
	if err != nil {
		a.fail(w, r, "/", "A Evolution recusou a criação da instância: "+err.Error())
		return
	}
	if err := a.store.SaveInstance(r.Context(), created.ID, created.Name, token); err != nil {
		a.fail(w, r, "/", "A instância foi criada, mas não foi possível guardar suas credenciais.")
		return
	}
	if err := a.store.SelectInstance(r.Context(), created.ID); err != nil {
		a.fail(w, r, "/", "A instância foi criada, mas não foi possível selecioná-la.")
		return
	}
	if err := a.evolution.ConnectInstance(r.Context(), token); err != nil {
		a.fail(w, r, "/", "A instância foi criada, mas não foi possível iniciá-la: "+err.Error())
		return
	}
	http.Redirect(w, r, "/pair", http.StatusSeeOther)
}

// pairData drives the pairing page. The page refreshes itself on a timer, so a
// scanned code moves the operator forward without any client-side scripting.
type pairData struct {
	Name, Code, Notice, Error string
	QRCode                    template.URL
	Connected                 bool
}

// qrImageSource accepts the QR code only as the inline PNG data URI Evolution
// is documented to produce. The value is remote input rendered into a src
// attribute, so anything else — another scheme, another media type, a stray
// character — is dropped rather than trusted.
func qrImageSource(image string) template.URL {
	const prefix = "data:image/png;base64,"
	payload, found := strings.CutPrefix(image, prefix)
	if !found || payload == "" {
		return ""
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return ""
	}
	return template.URL(prefix + payload)
}

func (a *webApp) pairPage(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	state := a.dashboardState(r)
	if state.Ready {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	d := pairData{Name: state.SelectedName, Error: r.URL.Query().Get("erro")}
	token, err := a.selectedToken(r)
	if err != nil {
		d.Notice = "Nenhuma instância deste painel está selecionada."
		a.render(w, "pair", d)
		return
	}
	code, err := a.evolution.QRCode(r.Context(), token)
	switch {
	case errors.Is(err, evolution.ErrLoggedIn):
		d.Connected = true
		d.Notice = "A sessão já está pareada. Aguardando a conexão ficar ativa."
	case err != nil:
		d.Notice = "O QR code ainda não está pronto. Esta página tenta de novo sozinha."
	default:
		d.QRCode, d.Code = qrImageSource(code.Image), code.Code
		if d.QRCode == "" && d.Code == "" {
			d.Notice = "O QR code ainda não está pronto. Esta página tenta de novo sozinha."
		}
	}
	a.render(w, "pair", d)
}

func (a *webApp) connectInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/pair", func(ctx context.Context, token string) error {
		return a.evolution.ConnectInstance(ctx, token)
	})
}

func (a *webApp) disconnectInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/", func(ctx context.Context, token string) error {
		return a.evolution.DisconnectInstance(ctx, token)
	})
}

func (a *webApp) logoutInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/", func(ctx context.Context, token string) error {
		return a.evolution.LogoutInstance(ctx, token)
	})
}

// instanceAction runs one Evolution operation against the selected instance,
// which is the only instance this panel ever touches.
func (a *webApp) instanceAction(w http.ResponseWriter, r *http.Request, destination string, run func(context.Context, string) error) {
	if !a.require(w, r) {
		return
	}
	token, err := a.selectedToken(r)
	if err != nil {
		a.fail(w, r, "/", "Selecione uma instância criada por este painel antes desta ação.")
		return
	}
	if err := run(r.Context(), token); err != nil {
		a.fail(w, r, "/", "A Evolution recusou a operação: "+err.Error())
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (a *webApp) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil || selected == "" {
		a.fail(w, r, "/", "Nenhuma instância selecionada.")
		return
	}
	if _, err := a.store.InstanceToken(r.Context(), selected); err != nil {
		a.fail(w, r, "/", "Este painel não gerencia a instância selecionada.")
		return
	}
	if err := a.evolution.DeleteInstance(r.Context(), selected); err != nil {
		a.fail(w, r, "/", "A Evolution recusou a remoção: "+err.Error())
		return
	}
	if err := a.store.ForgetInstance(r.Context(), selected); err != nil {
		a.fail(w, r, "/", "A instância foi removida na Evolution, mas o registro local permaneceu.")
		return
	}
	if err := a.store.SelectInstance(r.Context(), ""); err != nil {
		a.fail(w, r, "/", "A instância foi removida, mas a seleção não foi limpa.")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *webApp) selectInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	id := r.FormValue("instance_id")
	instances, err := a.evolution.FetchInstances(r.Context())
	if err != nil {
		http.Error(w, "Evolution indisponível", 503)
		return
	}
	valid := id == ""
	for _, instance := range instances {
		if instance.ID == id {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, "Instância inválida", 400)
		return
	}
	if err = a.store.SelectInstance(r.Context(), id); err != nil {
		http.Error(w, "Erro ao salvar", 500)
		return
	}
	http.Redirect(w, r, "/", 303)
}
func (a *webApp) selectedJSON(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil {
		http.Error(w, `{"status":"api_unavailable"}`, 500)
		return
	}
	status := "no_instance"
	name := ""
	instances, fetchErr := a.evolution.FetchInstances(r.Context())
	if fetchErr != nil {
		status = "api_unavailable"
	} else if len(instances) == 0 {
		status = "no_instance"
	} else if selected != "" {
		status = "disconnected"
		for _, i := range instances {
			if i.ID == selected {
				status = string(i.Status)
				name = i.Name
				break
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"selected_instance_id": selected, "name": name, "status": status})
}

// templateFuncs exposes the brand mark and small presentation helpers to the
// templates. The logo is trusted markup embedded in the binary, so it is
// inlined as template.HTML; every other value stays contextually escaped.
var templateFuncs = template.FuncMap{
	"logo":          brand.LogoSVG,
	"statusLabel":   statusLabel,
	"statusTone":    statusTone,
	"sessionLabel":  sessionLabel,
	"sessionTone":   sessionTone,
	"moment":        moment,
	"relativeSince": relativeSince,
}

// sessionLabel turns the WhatsApp session state into readable Portuguese. The
// distinctions matter operationally: a logged-out session needs a new QR code,
// while a disconnected one usually recovers on its own.
func sessionLabel(state string) string {
	switch state {
	case "connected":
		return "Conectado"
	case "pairing":
		return "Pareando"
	case "disconnected":
		return "Desconectado"
	case "logged_out":
		return "Sessão encerrada"
	case "banned":
		return "Conta banida"
	case "failed":
		return "Falha de conexão"
	}
	return "Desconhecido"
}

func sessionTone(state string) string {
	switch state {
	case "connected":
		return "ok"
	case "pairing":
		return "warn"
	}
	return "off"
}

// moment formats an instant for the panel, leaving an unset one blank rather
// than printing a zero date that reads as real data.
func moment(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Local().Format("02/01/2006 15:04")
}

// relativeSince says how long ago something happened, which is what tells the
// operator whether ingestion is alive without reading timestamps.
func relativeSince(at time.Time) string {
	if at.IsZero() {
		return "nunca"
	}
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "agora há pouco"
	case elapsed < time.Hour:
		return fmt.Sprintf("há %d min", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("há %d h", int(elapsed.Hours()))
	}
	return fmt.Sprintf("há %d dias", int(elapsed.Hours()/24))
}

// statusLabel turns an Evolution connection status into readable Portuguese.
func statusLabel(s evolution.ConnectionStatus) string {
	switch s {
	case evolution.StatusConnected:
		return "Conectada"
	case evolution.StatusConnecting:
		return "Conectando"
	case evolution.StatusDisconnected:
		return "Desconectada"
	}
	return "Desconhecida"
}

// statusTone maps a status to the pill modifier used by the stylesheet, so the
// class attribute never carries an unbounded value.
func statusTone(s evolution.ConnectionStatus) string {
	switch s {
	case evolution.StatusConnected:
		return "ok"
	case evolution.StatusConnecting:
		return "warn"
	}
	return "off"
}

const pages = `
{{define "head"}}<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if eq . "Conectar o WhatsApp"}}<meta http-equiv="refresh" content="5">{{end}}<title>{{.}} · WhatsApp MCP</title><style>
*,*::before,*::after{box-sizing:border-box}
:root{
color-scheme:light dark;
--bg:#eef4f1;--surface:#fff;--surface-soft:#f5faf8;--border:#d9e6e0;--text:#12291f;--muted:#5a7169;
--brand:#075e54;--brand-strong:#0b6f63;--brand-ink:#fff;
--danger:#8f1d1d;--danger-bg:#fdecea;--danger-border:#eec6c2;
--ok:#0d6b45;--ok-bg:#e2f4ec;--warn:#7a5200;--warn-bg:#fbf0d6;--off:#54696f;--off-bg:#e9eef0;
--radius:16px;--radius-sm:10px;--ring:#12a08c;--shadow:0 10px 30px rgba(18,41,31,.08);
}
@media (prefers-color-scheme:dark){:root{
--bg:#0b1714;--surface:#122420;--surface-soft:#17302a;--border:#264840;--text:#e6f2ed;--muted:#9db5ad;
--brand:#2ec49a;--brand-strong:#43d7ad;--brand-ink:#06231d;
--danger:#ffaba4;--danger-bg:#3b1a18;--danger-border:#6b3330;
--ok:#6fdcaa;--ok-bg:#113727;--warn:#f2ce85;--warn-bg:#382b10;--off:#a8bcc2;--off-bg:#1d2c30;
--shadow:0 10px 30px rgba(0,0,0,.35);
}}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
.page{max-width:980px;margin:0 auto;padding:clamp(20px,4vw,44px) clamp(16px,4vw,24px) 64px}
.page--narrow{max-width:520px;min-height:100vh;display:flex;flex-direction:column;justify-content:center;gap:20px}
h1,h2,h3{color:var(--brand);line-height:1.25;margin:0}
h1{font-size:clamp(1.4rem,1.1rem + 1.4vw,1.85rem)}
h2{font-size:1.2rem}
p{margin:.7em 0}
a{color:var(--brand-strong)}
.brand{display:flex;align-items:center;gap:14px;min-width:0}
.brand__mark{width:44px;height:44px;flex:none}
.brand__mark svg{width:100%;height:100%;display:block}
.brand__name{font-weight:700;font-size:1.1rem;color:var(--brand);letter-spacing:-.01em}
.brand__tagline{display:block;font-weight:400;font-size:.82rem;color:var(--muted);letter-spacing:0}
.topbar{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:16px;margin-bottom:24px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:clamp(20px,3vw,30px);margin:18px 0;box-shadow:var(--shadow)}
.card__head{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;margin-bottom:6px}
.muted{color:var(--muted);font-size:.92rem}
.field{display:block;margin:16px 0}
.field__label{display:block;font-weight:600;font-size:.92rem;margin-bottom:6px}
.field__hint{display:block;font-weight:400;color:var(--muted);font-size:.85rem}
input[type=text],input[type=password]{width:100%;font:inherit;padding:11px 13px;color:var(--text);background:var(--surface-soft);border:1px solid var(--border);border-radius:var(--radius-sm)}
input[type=text]:hover,input[type=password]:hover{border-color:var(--brand-strong)}
input[type=radio]{accent-color:var(--brand-strong);width:18px;height:18px;flex:none;margin:0}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;font:inherit;font-weight:600;text-decoration:none;padding:11px 18px;border:1px solid transparent;border-radius:var(--radius-sm);cursor:pointer;background:var(--brand);color:var(--brand-ink)}
.btn:hover{background:var(--brand-strong)}
.btn--block{width:100%}
.btn--ghost{background:transparent;color:var(--brand-strong);border-color:var(--border)}
.btn--ghost:hover{background:var(--surface-soft);border-color:var(--brand-strong)}
.btn--quiet{background:transparent;color:var(--muted);border-color:transparent;padding:8px 10px;font-weight:500}
.btn--quiet:hover{background:var(--surface-soft);color:var(--text)}
:where(a,button,input,summary):focus-visible{outline:3px solid var(--ring);outline-offset:2px}
.actions{display:flex;flex-wrap:wrap;gap:12px;align-items:center;margin-top:18px}
.alert{display:block;margin:16px 0;padding:12px 14px;border-radius:var(--radius-sm);border:1px solid var(--danger-border);background:var(--danger-bg);color:var(--danger);font-size:.94rem}
.instances{list-style:none;margin:18px 0 0;padding:0;border:1px solid var(--border);border-radius:var(--radius-sm);overflow:hidden}
.instance{display:flex;flex-wrap:wrap;align-items:center;gap:10px 14px;padding:14px 16px;background:var(--surface)}
.instance + .instance{border-top:1px solid var(--border)}
.instance--selected{background:var(--surface-soft)}
.instance__label{display:flex;align-items:center;gap:12px;flex:1 1 220px;min-width:0;cursor:pointer}
.instance__name{font-weight:600;overflow-wrap:anywhere}
.instance__number{display:block;font-weight:400;font-size:.85rem;color:var(--muted)}
.pill{display:inline-flex;align-items:center;gap:7px;font-size:.82rem;font-weight:600;padding:4px 11px;border-radius:999px;white-space:nowrap}
.pill::before{content:"";width:8px;height:8px;border-radius:50%;background:currentColor}
.pill--ok{background:var(--ok-bg);color:var(--ok)}
.pill--warn{background:var(--warn-bg);color:var(--warn)}
.pill--off{background:var(--off-bg);color:var(--off)}
.pill--current{background:var(--ok-bg);color:var(--ok)}
.pill--current::before{display:none}
.empty{margin:18px 0 0;padding:28px 20px;text-align:center;border:1px dashed var(--border);border-radius:var(--radius-sm);background:var(--surface-soft)}
.link-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:10px;margin:20px 0}.link-grid a{display:flex;align-items:center;min-height:44px;padding:10px 12px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-soft);font-weight:600;text-decoration:none}.link-grid a:hover{border-color:var(--brand-strong);background:var(--surface)}
.empty__title{font-weight:600;color:var(--text)}
.subhead{font-size:1rem;margin-top:22px}
.problems{margin:14px 0;padding:12px 14px 12px 32px;border-radius:var(--radius-sm);border:1px solid var(--danger-border);background:var(--danger-bg);color:var(--danger)}
.problems li+li{margin-top:6px}
.facts{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:14px;margin:18px 0 0}
.fact dt{font-size:.82rem;color:var(--muted);font-weight:600}
.fact dd{margin:2px 0 0;font-weight:600}
.fact__detail{display:block;font-weight:400;font-size:.82rem;color:var(--muted)}
.queues{list-style:none;margin:12px 0 0;padding:0;display:grid;gap:8px}
.queue{display:flex;flex-wrap:wrap;align-items:center;gap:8px 12px;padding:10px 12px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-soft)}
.queue__name{font-weight:600;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.9rem}
.secret{margin:18px 0;padding:16px;border:1px solid var(--ok);border-radius:var(--radius-sm);background:var(--ok-bg)}
.secret__title{margin:0 0 8px;font-weight:700;color:var(--ok)}
.keys{list-style:none;margin:12px 0 0;padding:0;display:grid;gap:8px}
.key{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px 12px;padding:10px 12px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-soft)}
.key__name{font-weight:600}
.key__prefix{display:block;font-weight:400;font-size:.82rem;color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.qrcode{display:block;margin:0 auto;width:256px;height:256px;max-width:100%;background:#fff;padding:10px;border-radius:var(--radius-sm);border:1px solid var(--border)}
code{background:var(--surface-soft);border:1px solid var(--border);padding:2px 6px;border-radius:6px;font-size:.88em;overflow-wrap:anywhere}
pre{overflow:auto;background:#0c241e;color:#e8fff7;padding:16px;border-radius:var(--radius-sm);font-size:.88rem;line-height:1.5}
pre code{background:none;border:0;padding:0;color:inherit}
.sr-only{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
@media (max-width:480px){.instance{align-items:flex-start}.instance form,.instance .btn{width:100%}}
</style></head><body>{{end}}

{{define "brand"}}<div class="brand"><span class="brand__mark">{{logo}}</span><span class="brand__name">WhatsApp MCP<span class="brand__tagline">Painel de controle</span></span></div>{{end}}

{{define "setup"}}{{template "head" "Configuração inicial"}}<main class="page page--narrow">
{{template "brand"}}
<section class="card"><h1>Configuração inicial</h1>
<p class="muted">Crie o único administrador deste painel. Depois disso, esta página deixa de aceitar novos cadastros.</p>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<form method="post">
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha<span class="field__hint">Mínimo de 10 caracteres.</span></span></label>
<input id="password" type="password" name="password" minlength="10" required autocomplete="new-password">
<div class="actions"><button class="btn btn--block" type="submit">Criar administrador</button></div>
</form></section>
<p class="muted">As senhas são armazenadas com bcrypt no PostgreSQL e nunca aparecem nos logs.</p>
</main></body></html>{{end}}

{{define "login"}}{{template "head" "Entrar"}}<main class="page page--narrow">
{{template "brand"}}
<section class="card"><h1>Entrar</h1>
<p class="muted">Informe as credenciais do administrador para gerenciar a instância conectada.</p>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<form method="post">
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha</span></label>
<input id="password" type="password" name="password" required autocomplete="current-password">
<div class="actions"><button class="btn btn--block" type="submit">Entrar</button></div>
</form></section>
<p class="muted">A sessão expira em 24 horas e é invalidada quando o processo reinicia.</p>
</main></body></html>{{end}}

{{define "dashboard"}}{{template "head" "Painel"}}<main class="page">
<header class="topbar">{{template "brand"}}<form method="post" action="/logout"><button class="btn btn--quiet" type="submit">Sair</button></form></header>

{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}

<section class="card" aria-labelledby="instancia">
<div class="card__head"><h2 id="instancia">Instância do WhatsApp</h2></div>
<p class="muted">Crie, conecte e remova instâncias aqui mesmo. Este painel fala com o WhatsApp por dentro; nenhuma outra interface é necessária.</p>

{{if .Unavailable}}<p class="alert" role="alert">{{.Notice}}</p>
{{else}}
{{if .Instances}}
<form method="post" action="/selection">
<ul class="instances">
{{range .Instances}}<li class="instance{{if .Selected}} instance--selected{{end}}">
<label class="instance__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if .Selected}} checked{{end}}>
<span class="instance__name">{{.Name}}{{if .Number}}<span class="instance__number">{{.Number}}</span>{{end}}</span>
</label>
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
{{if .Selected}}<span class="pill pill--current">Em uso pelo MCP</span>{{end}}
{{if not .Managed}}<span class="pill pill--off">Fora deste painel</span>{{end}}
</li>{{end}}
</ul>
<div class="actions"><button class="btn" type="submit">Usar esta instância</button></div>
</form>
{{else}}
<div class="empty"><p class="empty__title">Nenhuma instância ainda</p><p class="muted">{{.Notice}}</p></div>
{{end}}

<form method="post" action="/instances">
<label class="field" for="new-instance"><span class="field__label">Criar uma instância<span class="field__hint">Um nome para identificar esta conta de WhatsApp.</span></span></label>
<input id="new-instance" type="text" name="name" maxlength="60" required placeholder="pessoal" autocapitalize="none" spellcheck="false">
<div class="actions"><button class="btn" type="submit">Criar e conectar</button></div>
</form>

{{if .NeedsPairing}}<div class="actions"><a class="btn" href="/pair">Conectar o WhatsApp</a></div>{{end}}

{{if .Selected}}
<div class="actions">
<form method="post" action="/instances/disconnect"><button class="btn btn--ghost" type="submit">Desconectar</button></form>
<form method="post" action="/instances/logout"><button class="btn btn--ghost" type="submit">Encerrar sessão do WhatsApp</button></form>
<form method="post" action="/instances/delete"><button class="btn btn--ghost" type="submit">Remover instância</button></form>
</div>
<p class="muted">Desconectar apenas para o cliente e preserva o pareamento. Encerrar a sessão exige um novo QR code. Remover apaga a instância na Evolution.</p>
{{end}}
{{end}}
<p class="muted">O MCP usa no máximo uma instância. Selecionar outra substitui a seleção atual.</p>
</section>

{{with .Status}}
<section class="card" aria-labelledby="estado">
<div class="card__head"><h2 id="estado">Estado do serviço</h2>
<span class="pill pill--{{sessionTone .WhatsApp.State}}">{{sessionLabel .WhatsApp.State}}</span></div>

{{if .Problems}}
<ul class="problems">{{range .Problems}}<li>{{.}}</li>{{end}}</ul>
{{else}}
<p class="muted">Nenhum problema detectado. As mensagens estão sendo recebidas e indexadas.</p>
{{end}}

<dl class="facts">
<div class="fact"><dt>Último evento recebido</dt><dd>{{relativeSince .LastEvent}}<span class="fact__detail">{{moment .LastEvent}}</span></dd></div>
{{if .WhatsApp.PushName}}<div class="fact"><dt>Conta conectada</dt><dd>{{.WhatsApp.PushName}}</dd></div>{{end}}
{{if .HasIndex}}
<div class="fact"><dt>Mensagens indexadas</dt><dd>{{.Coverage.Messages}}</dd></div>
<div class="fact"><dt>Histórico desde</dt><dd>{{moment .Coverage.OldestAt}}</dd></div>
{{end}}
</dl>

{{if .WhatsApp.Reason}}<p class="muted">Motivo informado pelo WhatsApp: <code>{{.WhatsApp.Reason}}</code></p>{{end}}

{{if .Queues}}
<h3 class="subhead">Filas de ingestão</h3>
<ul class="queues">
{{range .Queues}}<li class="queue">
<span class="queue__name">{{.Name}}</span>
<span class="pill pill--{{if .Consuming}}ok{{else}}off{{end}}">{{if .Consuming}}Consumindo{{else}}Parada{{end}}</span>
<span class="muted">{{.Delivered}} eventos{{if .Rejected}} · {{.Rejected}} rejeitados{{end}} · {{relativeSince .LastEventAt}}</span>
</li>{{end}}
</ul>
<p class="muted">Uma fila parada acumula mensagens no broker sem que nada seja indexado.</p>
{{end}}
</section>
{{end}}

<section class="card" aria-labelledby="configuracao">
{{if .Ready}}
<h2 id="configuracao">Conectar o Claude a este MCP</h2>
<p>O WhatsApp está conectado na instância <strong>{{.SelectedName}}</strong>. Gere uma chave de API e use a configuração abaixo. A chave é a única informação que o cliente precisa: ela já identifica a conta e a instância.</p>

{{with .NewKey}}
<div class="secret">
<p class="secret__title">Sua nova chave — copie agora</p>
<pre><code>{{.}}</code></pre>
<p class="muted">Ela não será exibida novamente. Guarde no gerenciador de credenciais do cliente, nunca em um prompt ou arquivo versionado.</p>
</div>
<h3 class="subhead">Configuração do cliente</h3>
<pre><code>{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "{{$.Endpoint}}",
      "headers": {
        "Authorization": "Bearer {{.}}"
      }
    }
  }
}</code></pre>
<p class="muted">No Claude Code: <code>claude mcp add --transport http whatsapp {{$.Endpoint}} --header "Authorization: Bearer SUA_CHAVE"</code></p>
{{else}}
<h3 class="subhead">Configuração do cliente</h3>
<pre><code>{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "{{.Endpoint}}",
      "headers": {
        "Authorization": "Bearer SUA_CHAVE"
      }
    }
  }
}</code></pre>
{{end}}

<h3 class="subhead">Chaves de API</h3>
{{if .Keys}}
<ul class="keys">
{{range .Keys}}<li class="key">
<span class="key__name">{{.Name}}<span class="key__prefix">{{.Prefix}}…</span></span>
<span class="muted">criada em {{moment .CreatedAt}} · uso {{relativeSince .LastUsedAt}}</span>
<form method="post" action="/keys/revoke"><input type="hidden" name="id" value="{{.ID}}"><button class="btn btn--quiet" type="submit">Revogar</button></form>
</li>{{end}}
</ul>
{{else}}
<p class="muted">Nenhuma chave ativa. Crie uma para conectar um cliente.</p>
{{end}}
<form method="post" action="/keys">
<label class="field" for="key-name"><span class="field__label">Nova chave<span class="field__hint">Um nome para lembrar onde ela está sendo usada.</span></span></label>
<input id="key-name" type="text" name="name" maxlength="60" placeholder="claude code do notebook" autocapitalize="none" spellcheck="false">
<div class="actions"><button class="btn" type="submit">Gerar chave</button></div>
</form>
<p class="muted">Revogar tem efeito imediato: cada requisição do MCP é autenticada por conta própria.</p>

<h3 class="subhead">Histórico</h3>
<p class="muted">O índice cobre o que chegou desde que esta instância foi conectada. O WhatsApp devolve mensagens anteriores a uma que ele já conhece, então cada pedido recua mais um trecho.</p>
<form method="post" action="/instances/history"><div class="actions"><button class="btn btn--ghost" type="submit">Puxar mensagens mais antigas</button></div></form>
{{else}}
<h2 id="configuracao">Prepare o WhatsApp antes de configurar o MCP</h2>
<p>A configuração do Claude fica disponível quando houver uma instância selecionada e conectada.</p>
<div class="empty"><p class="empty__title">Instância ainda não pronta</p><p class="muted">{{.Notice}}</p>{{if .NeedsPairing}}<div class="actions"><a class="btn" href="/pair">Conectar o WhatsApp</a></div>{{end}}</div>
{{end}}
<div class="link-grid"><a href="/">Painel do MCP</a><a href="/healthz">Health do MCP</a><a href="/readyz">Readiness do MCP</a><a href="/api/selected-instance">Instância selecionada (autenticado)</a></div>
</section>
</main></body></html>{{end}}

{{define "pair"}}{{template "head" "Conectar o WhatsApp"}}<main class="page page--narrow">
{{template "brand"}}
<section class="card"><h1>Conectar o WhatsApp</h1>
{{with .Name}}<p class="muted">Instância <strong>{{.}}</strong>.</p>{{end}}
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{if .QRCode}}
<p>No celular, abra o WhatsApp em <strong>Dispositivos conectados</strong>, toque em <strong>Conectar um dispositivo</strong> e aponte a câmera para o código.</p>
<p><img class="qrcode" src="{{.QRCode}}" alt="QR code para conectar o WhatsApp" width="256" height="256"></p>
{{else}}
<div class="empty"><p class="empty__title">Aguardando o QR code</p><p class="muted">{{.Notice}}</p></div>
{{end}}
{{with .Code}}<p class="muted">Código: <code>{{.}}</code></p>{{end}}
<div class="actions">
<form method="post" action="/instances/connect"><button class="btn btn--ghost" type="submit">Gerar outro código</button></form>
<a class="btn btn--quiet" href="/">Voltar ao painel</a>
</div>
</section>
<p class="muted">Esta página se atualiza sozinha a cada 5 segundos. O código expira rápido; se ele sumir, gere outro.</p>
</main></body></html>{{end}}`
