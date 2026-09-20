package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/ratelimit"
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
	AdminMustChangePassword(context.Context) (bool, error)
	SetAdminPassword(context.Context, string) error
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
	// setupToken guards the first-run form. Empty leaves it unguarded, which is
	// right for loopback and wrong for anything the installer published.
	setupToken string
	sessions   *sessions
	templates  *template.Template
	// logins throttles password guessing. The administrator password is the
	// weakest credential the gateway holds — a person chose it — and it opens
	// the panel that owns the WhatsApp session, so the login form needs the same
	// per-address lockout the MCP endpoint already has. bcrypt slows one guess
	// down; only a lockout stops a campaign of them.
	logins *ratelimit.Attempts
}

const (
	loginFailures = 8
	loginLockout  = 5 * time.Minute
)

func NewWebHandler(store ControlStore, client EvolutionAPI, status StatusReader, sessionKey []byte, publicURL, setupToken string) http.Handler {
	a := &webApp{store: store, evolution: client, status: status, publicURL: strings.TrimRight(publicURL, "/"), setupToken: setupToken, sessions: newSessions(sessionKey), templates: template.Must(template.New("pages").Funcs(templateFuncs).Parse(pages)), logins: ratelimit.New(loginFailures, loginLockout)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.connect)
	mux.HandleFunc("GET /setup", a.setupPage)
	mux.HandleFunc("POST /setup", a.setup)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /senha", a.passwordPage)
	mux.HandleFunc("POST /senha", a.changePassword)
	mux.HandleFunc("GET /instancias", a.instances)
	mux.HandleFunc("POST /instancias", a.createInstance)
	mux.HandleFunc("POST /instancias/selecionar", a.selectInstance)
	mux.HandleFunc("POST /instancias/conectar", a.connectInstance)
	mux.HandleFunc("POST /instancias/desconectar", a.disconnectInstance)
	mux.HandleFunc("POST /instancias/sair", a.logoutInstance)
	mux.HandleFunc("POST /instancias/remover", a.deleteInstance)
	mux.HandleFunc("POST /instancias/historico", a.syncHistory)
	mux.HandleFunc("GET /estado", a.statusPage)
	mux.HandleFunc("GET /documentacao", a.docs)
	mux.HandleFunc("GET /receitas", a.recipes)
	mux.HandleFunc("GET /pair", a.pairPage)
	mux.HandleFunc("POST /chaves", a.createKey)
	mux.HandleFunc("POST /chaves/revogar", a.revokeKey)
	mux.Handle("GET /assets/", assetHandler())
	for path, ico := range iconRoutes() {
		mux.HandleFunc(path, iconHandler(ico))
	}
	mux.HandleFunc("GET /api/selected-instance", a.selectedJSON)
	mux.HandleFunc("GET /api/progresso", a.progress)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// data: is required for img-src because the pairing QR code arrives from
		// Evolution as an inline data URI; no other source is allowed.
		// frame-ancestors and form-action are the two that matter for a panel
		// whose buttons revoke keys and unlink a phone: nothing may frame it,
		// and no injected markup may aim a form at another origin. base-uri
		// stops a stray <base> from re-pointing every relative URL on the page.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
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
	if !a.authenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return false
	}
	// An installer-generated password was printed to a terminal and may be in a
	// shell history, a screenshot or a support thread. Until it is replaced the
	// panel does nothing else, so a password that leaked in transit buys an
	// attacker a password form rather than a WhatsApp session.
	if must, err := a.store.AdminMustChangePassword(r.Context()); err == nil && must {
		http.Redirect(w, r, "/senha", http.StatusSeeOther)
		return false
	}
	return true
}

func (a *webApp) passwordPage(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.render(w, "senha", a.passwordLayout(r))
}

// passwordLayout decides which of the two pages this is. Forced means the
// installer's password is still in place and nothing else will render;
// otherwise it is an ordinary page with the panel's chrome around it.
func (a *webApp) passwordLayout(r *http.Request) passwordPageData {
	must, err := a.store.AdminMustChangePassword(r.Context())
	if err == nil && must {
		return passwordPageData{layout: layout{Title: "Defina uma senha"}, Forced: true}
	}
	return passwordPageData{layout: a.newLayout(r, "Trocar a senha", ""), Saved: r.URL.Query().Get("ok")}
}

func (a *webApp) changePassword(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_, hash, err := a.admin(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	forced, _ := a.store.AdminMustChangePassword(r.Context())
	current := r.FormValue("current_password")
	next := r.FormValue("password")
	// The current password is asked for even though the session already proves
	// who this is: a session left open on a shared machine should not be enough
	// to lock the owner out of their own panel.
	if !auth.CheckPassword(hash, current) {
		w.WriteHeader(http.StatusUnauthorized)
		page := a.passwordLayout(r)
		page.Error = "A senha atual não confere."
		a.render(w, "senha", page)
		return
	}
	if len(next) < 10 || len([]byte(next)) > 72 {
		page := a.passwordLayout(r)
		page.Error = "Use uma senha com pelo menos 10 caracteres."
		a.render(w, "senha", page)
		return
	}
	if next != r.FormValue("confirm_password") {
		page := a.passwordLayout(r)
		page.Error = "As duas senhas não são iguais."
		a.render(w, "senha", page)
		return
	}
	if auth.CheckPassword(hash, next) {
		page := a.passwordLayout(r)
		page.Error = "Escolha uma senha diferente da atual."
		a.render(w, "senha", page)
		return
	}
	newHash, err := auth.HashPassword(next)
	if err != nil {
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}
	if err := a.store.SetAdminPassword(r.Context(), newHash); err != nil {
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}
	// A forced change is on its way somewhere: the panel was refusing to serve
	// anything else, so land on the panel. A voluntary one should say it worked.
	if forced {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/senha?ok="+url.QueryEscape("Senha alterada."), http.StatusSeeOther)
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
	// The token arrives in the link the installer printed, so there is nothing
	// for anyone to copy or keep. It lives in the query only for this first
	// request: the form carries it in a hidden field so the POST does not put
	// it back in the address bar, and Referrer-Policy keeps it out of any
	// outbound request. It stops meaning anything the moment an administrator
	// exists, which is the request right after this one.
	page := setupPageData{layout: layout{Title: "Configuração inicial"}}
	if a.setupToken != "" {
		presented := strings.TrimSpace(r.URL.Query().Get("token"))
		if subtle.ConstantTimeCompare([]byte(presented), []byte(a.setupToken)) != 1 {
			page.Locked = true
			w.WriteHeader(http.StatusForbidden)
			a.render(w, "setup", page)
			return
		}
		page.Token = a.setupToken
	}
	a.render(w, "setup", page)
}
func (a *webApp) setup(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.admin(r); err == nil {
		http.Error(w, "O administrador já existe.", 409)
		return
	}
	// The token is what closes the window between the installer printing a
	// public URL and the operator reaching it. Without this, whoever arrives
	// first becomes the administrator of somebody else's WhatsApp session.
	if a.setupToken != "" {
		source := ratelimit.ClientIP(r)
		if !a.logins.Allow(source) {
			w.Header().Set("Retry-After", "300")
			w.WriteHeader(http.StatusTooManyRequests)
			a.render(w, "setup", setupPageData{layout: layout{Title: "Configuração inicial", Error: "Tentativas demais. Espere alguns minutos e tente de novo."}, Locked: true})
			return
		}
		presented := strings.TrimSpace(r.FormValue("setup_token"))
		if subtle.ConstantTimeCompare([]byte(presented), []byte(a.setupToken)) != 1 {
			a.logins.Fail(source)
			w.WriteHeader(http.StatusForbidden)
			a.render(w, "setup", setupPageData{layout: layout{Title: "Configuração inicial", Error: "O link de instalação não confere. Use o link completo que o instalador imprimiu."}, Locked: true})
			return
		}
		a.logins.Succeed(source)
	}
	u := strings.TrimSpace(r.FormValue("username"))
	p := r.FormValue("password")
	if u == "" || len(p) < 10 || len([]byte(p)) > 72 {
		a.render(w, "setup", setupPageData{layout: layout{Title: "Configuração inicial", Error: "Use um usuário e uma senha com pelo menos 10 caracteres."}, Token: a.setupToken})
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
	a.render(w, "login", layout{Title: "Entrar"})
}
func (a *webApp) login(w http.ResponseWriter, r *http.Request) {
	u, h, err := a.admin(r)
	if err != nil {
		http.Redirect(w, r, "/setup", 303)
		return
	}
	source := ratelimit.ClientIP(r)
	if !a.logins.Allow(source) {
		w.Header().Set("Retry-After", "300")
		w.WriteHeader(http.StatusTooManyRequests)
		a.render(w, "login", layout{Title: "Entrar", Error: "Tentativas demais. Espere alguns minutos e tente de novo."})
		return
	}
	if !hmac.Equal([]byte(u), []byte(strings.TrimSpace(r.FormValue("username")))) || !auth.CheckPassword(h, r.FormValue("password")) {
		a.logins.Fail(source)
		w.WriteHeader(401)
		a.render(w, "login", layout{Title: "Entrar", Error: "Usuário ou senha inválidos."})
		return
	}
	a.logins.Succeed(source)
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

// layout carries what every page needs: which tab is current, an error to show
// once, and the session pill that keeps the WhatsApp state visible from any
// page without repeating the whole status panel.
type layout struct {
	Title        string
	Active       string
	Error        string
	Refresh      bool
	SessionLabel string
	SessionTone  string
}

// setupPageData tells the first-run form whether to ask for the token.
type setupPageData struct {
	layout
	// Token is echoed into a hidden field so the POST carries it without the
	// address bar doing so. Locked means the visitor arrived without the link
	// the installer printed, and the form is not offered at all.
	Token  string
	Locked bool
}

// passwordPageData is the password page's own shape rather than two more
// fields on layout: instancesPage embeds both layout and selection, selection
// already carries a Notice, and a second one at the same depth makes the
// selector ambiguous for every page that embeds both.
type passwordPageData struct {
	layout
	// Forced marks this as the only page the panel will serve, which is the
	// installer's temporary password still standing. Reached from the masthead
	// instead, the same page is a choice rather than a gate.
	Forced bool
	Saved  string
}

// newLayout builds the chrome shared by every signed-in page.
func (a *webApp) newLayout(r *http.Request, title, active string) layout {
	page := layout{Title: title, Active: active, Error: r.URL.Query().Get("erro"), SessionTone: "off"}
	if a.status != nil {
		state := a.status.Snapshot().WhatsApp.State
		page.SessionLabel, page.SessionTone = sessionLabel(state), sessionTone(state)
	}
	return page
}

// instanceView is one row of the instances page. Managed marks the instances
// this panel holds credentials for, which are the only ones it can operate.
type instanceView struct {
	evolution.Instance
	Managed  bool
	Selected bool
}

// selection is what the panel knows about the chosen instance, shared by the
// pages that need it.
type selection struct {
	Instances    []instanceView
	Selected     string
	SelectedName string
	Notice       string
	Unavailable  bool
	Ready        bool
	NeedsPairing bool
}

// readSelection lists the instances and works out which one the MCP is using.
// Adoption happens here: Evolution's listing carries each instance's token, so
// an instance created before this panel existed can be taken over instead of
// paired again.
func (a *webApp) readSelection(r *http.Request) selection {
	selected, _ := a.store.SelectedInstance(r.Context())
	managed, _ := a.store.ManagedInstances(r.Context())
	instances, err := a.evolution.FetchInstances(r.Context())
	state := selection{Selected: selected, Unavailable: err != nil}
	if err != nil {
		state.Notice = "A API do WhatsApp está indisponível no momento. Tente novamente em instantes."
		return state
	}
	for _, instance := range instances {
		_, isManaged := managed[instance.ID]
		if !isManaged && instance.Token != "" {
			if err := a.store.SaveInstance(r.Context(), instance.ID, instance.Name, instance.Token); err == nil {
				isManaged = true
			}
		}
		view := instanceView{Instance: instance, Managed: isManaged, Selected: instance.ID == selected}
		if view.Selected {
			state.SelectedName = instance.Name
			switch {
			case instance.Status == evolution.StatusConnected:
				state.Ready = true
			case isManaged:
				state.NeedsPairing = true
			}
		}
		state.Instances = append(state.Instances, view)
	}
	switch {
	case len(state.Instances) == 0:
		state.Notice = "Nenhuma instância ainda. Adicione a primeira e conecte o WhatsApp sem sair deste painel."
	case selected == "":
		state.Notice = "Escolha qual instância o MCP deve usar."
	case state.NeedsPairing:
		state.Notice = "A instância selecionada ainda não está pareada. Leia o QR code para conectar o WhatsApp."
	case !state.Ready:
		state.Notice = "Este painel não tem credenciais da instância selecionada, então não pode operá-la."
	}
	return state
}

// statusView is the operational picture: what WhatsApp reports, what the
// ingestion is doing, and how much history is indexed.
type statusView struct {
	WhatsApp  health.WhatsApp
	Queues    []health.Queue
	Problems  []string
	Coverage  store.Coverage
	HasIndex  bool
	LastEvent time.Time
}

// readStatus pairs the live snapshot with the index coverage. The coverage
// query can fail while the database is the thing that is broken, which must not
// stop the rest of the status from rendering.
func (a *webApp) readStatus(r *http.Request, selected string) *statusView {
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

// connectPage is the landing page: everything needed to point a client at this
// gateway, and nothing else.
type connectPage struct {
	layout
	Ready        bool
	NeedsPairing bool
	Notice       string
	InstanceName string
	Endpoint     string
	Keys         []store.APIKey
	// HasKey and ClientConnected drive the checklist. They are facts the panel
	// already holds rather than a stored notion of progress: a key exists or it
	// does not, and a key that has been used proves a client authenticated with
	// it. Revoking the last key therefore reopens the first step on its own.
	HasKey          bool
	ClientConnected bool
	LastUse         time.Time
	Setup           clientSetup
	Prompts         []string
}

// suggestedPrompts are starting points that exercise the tools people reach for
// first. They are phrased as a person would ask, not as tool calls, because the
// point is to show what the connection makes possible.
var suggestedPrompts = []string{
	"Qual é o número de telefone conectado no meu WhatsApp?",
	"Liste minhas 10 conversas mais recentes do WhatsApp.",
	"Me resuma a conversa do WhatsApp com o João da Silva de hoje.",
	"Procure no meu WhatsApp as mensagens que falam sobre contrato.",
	"Quais grupos do WhatsApp eu participo? Quem são os administradores do maior deles?",
}

func (a *webApp) connect(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	state := a.readSelection(r)
	page := connectPage{
		layout:       a.newLayout(r, "Conectar", "conectar"),
		Ready:        state.Ready,
		NeedsPairing: state.NeedsPairing,
		Notice:       state.Notice,
		InstanceName: state.SelectedName,
		Endpoint:     a.endpoint(),
	}
	page.Keys, _ = a.store.ListAPIKeys(r.Context())
	page.Setup = newClientSetup(page.Endpoint, "")
	page.Prompts = suggestedPrompts
	page.HasKey = len(page.Keys) > 0
	for _, key := range page.Keys {
		if key.LastUsedAt.After(page.LastUse) {
			page.LastUse, page.ClientConnected = key.LastUsedAt, true
		}
	}
	a.render(w, "conectar", page)
}

// instancesPage manages the WhatsApp accounts themselves.
type instancesPage struct {
	layout
	selection
}

func (a *webApp) instances(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	a.render(w, "instancias", instancesPage{layout: a.newLayout(r, "Instâncias", "instancias"), selection: a.readSelection(r)})
}

// statusPage is the diagnostic view.
type statusPage struct {
	layout
	Status *statusView
}

func (a *webApp) statusPage(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, _ := a.store.SelectedInstance(r.Context())
	a.render(w, "estado", statusPage{layout: a.newLayout(r, "Estado", "estado"), Status: a.readStatus(r, selected)})
}

// endpoint is the address clients are told to use.
func (a *webApp) endpoint() string { return a.publicURL + "/mcp" }

// selectedToken resolves the Evolution credential for the selected instance.
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
// error text carries its own wording, so it travels as a query value and is
// escaped by the template rather than interpolated into markup here.
func (a *webApp) fail(w http.ResponseWriter, r *http.Request, path, reason string) {
	http.Redirect(w, r, path+"?erro="+url.QueryEscape(reason), http.StatusSeeOther)
}

func (a *webApp) createInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 60 {
		a.fail(w, r, "/instancias", "Informe um nome de instância com até 60 caracteres.")
		return
	}
	token, err := newInstanceToken()
	if err != nil {
		a.fail(w, r, "/instancias", "Não foi possível gerar as credenciais da instância.")
		return
	}
	created, err := a.evolution.CreateInstance(r.Context(), name, token)
	if err != nil {
		a.fail(w, r, "/instancias", "O WhatsApp recusou a criação da instância: "+err.Error())
		return
	}
	if err := a.store.SaveInstance(r.Context(), created.ID, created.Name, token); err != nil {
		a.fail(w, r, "/instancias", "A instância foi criada, mas não foi possível guardar suas credenciais.")
		return
	}
	if err := a.store.SelectInstance(r.Context(), created.ID); err != nil {
		a.fail(w, r, "/instancias", "A instância foi criada, mas não foi possível selecioná-la.")
		return
	}
	if err := a.evolution.ConnectInstance(r.Context(), token); err != nil {
		a.fail(w, r, "/instancias", "A instância foi criada, mas não foi possível iniciá-la: "+err.Error())
		return
	}
	http.Redirect(w, r, "/pair", http.StatusSeeOther)
}

func (a *webApp) selectInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	id := r.FormValue("instance_id")
	instances, err := a.evolution.FetchInstances(r.Context())
	if err != nil {
		a.fail(w, r, "/instancias", "A API do WhatsApp está indisponível no momento.")
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
		http.Error(w, "Instância inválida", http.StatusBadRequest)
		return
	}
	if err = a.store.SelectInstance(r.Context(), id); err != nil {
		a.fail(w, r, "/instancias", "Não foi possível salvar a seleção.")
		return
	}
	http.Redirect(w, r, "/instancias", http.StatusSeeOther)
}

// pairPageData drives the pairing screen, which refreshes itself so a scanned
// code moves the operator forward without any client-side scripting.
type pairPageData struct {
	layout
	Name, Notice string
	QRCode       template.URL
}

// qrImageSource accepts the QR only as the inline PNG data URI Evolution is
// documented to produce. The value is remote input rendered into a src
// attribute, so anything else is dropped rather than trusted.
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
	state := a.readSelection(r)
	if state.Ready {
		http.Redirect(w, r, "/instancias", http.StatusSeeOther)
		return
	}
	page := pairPageData{layout: a.newLayout(r, "Conectar o WhatsApp", "instancias"), Name: state.SelectedName}
	page.Refresh = true
	token, err := a.selectedToken(r)
	if err != nil {
		page.Notice = "Nenhuma instância deste painel está selecionada."
		a.render(w, "pair", page)
		return
	}
	code, err := a.evolution.QRCode(r.Context(), token)
	switch {
	case errors.Is(err, evolution.ErrLoggedIn):
		page.Notice = "A sessão já está pareada. Aguardando a conexão ficar ativa."
	case err != nil:
		page.Notice = "O QR code ainda não está pronto. Esta página tenta de novo sozinha."
	default:
		page.QRCode = qrImageSource(code.Image)
		if page.QRCode == "" {
			page.Notice = "O QR code ainda não está pronto. Esta página tenta de novo sozinha."
		}
	}
	a.render(w, "pair", page)
}

func (a *webApp) connectInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/pair", func(ctx context.Context, token string) error {
		return a.evolution.ConnectInstance(ctx, token)
	})
}

func (a *webApp) disconnectInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/instancias", func(ctx context.Context, token string) error {
		return a.evolution.DisconnectInstance(ctx, token)
	})
}

func (a *webApp) logoutInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, "/instancias", func(ctx context.Context, token string) error {
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
		a.fail(w, r, "/instancias", "Selecione uma instância deste painel antes desta ação.")
		return
	}
	if err := run(r.Context(), token); err != nil {
		a.fail(w, r, "/instancias", "O WhatsApp recusou a operação: "+err.Error())
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

// deleteInstance removes the instance the form names, and the one in use when
// it names none. Naming it is what lets every row carry its own remove button:
// the operator removes the account they pointed at, not whatever happened to be
// selected, and the confirmation can say which account that is.
func (a *webApp) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil {
		a.fail(w, r, "/instancias", "Não foi possível ler a instância em uso.")
		return
	}
	target := strings.TrimSpace(r.FormValue("instance_id"))
	if target == "" {
		target = selected
	}
	if target == "" {
		a.fail(w, r, "/instancias", "Nenhuma instância selecionada.")
		return
	}
	if _, err := a.store.InstanceToken(r.Context(), target); err != nil {
		a.fail(w, r, "/instancias", "Este painel não gerencia essa instância.")
		return
	}
	if err := a.evolution.DeleteInstance(r.Context(), target); err != nil {
		a.fail(w, r, "/instancias", "O WhatsApp recusou a remoção: "+err.Error())
		return
	}
	if err := a.store.ForgetInstance(r.Context(), target); err != nil {
		a.fail(w, r, "/instancias", "A instância foi removida, mas o registro local permaneceu.")
		return
	}
	// Only the instance in use leaves the MCP without a selection.
	if target == selected {
		if err := a.store.SelectInstance(r.Context(), ""); err != nil {
			a.fail(w, r, "/instancias", "A instância foi removida, mas a seleção não foi limpa.")
			return
		}
	}
	http.Redirect(w, r, "/instancias", http.StatusSeeOther)
}

func (a *webApp) selectedJSON(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil {
		http.Error(w, `{"status":"api_unavailable"}`, http.StatusInternalServerError)
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
	"author":        func() string { return brand.Author },
	"authorURL":     func() string { return brand.AuthorURL },
	"repositoryURL": func() string { return brand.RepositoryURL },
	"supportURL":    func() string { return brand.SupportURL },
	"statusLabel":   statusLabel,
	"statusTone":    statusTone,
	"sessionLabel":  sessionLabel,
	"sessionTone":   sessionTone,
	"moment":        moment,
	"relativeSince": relativeSince,
	"plural":        plural,
	"count":         count,
	"len64":         len64,
}

// len64 gives templates a length the counters can consume, since plural counts
// in int64 and the template package has no conversion of its own.
func len64(values []store.APIKey) int64 { return int64(len(values)) }

// plural keeps the counters readable. "1 eventos" is the kind of detail that
// makes a panel look unfinished.
func plural(quantity int64, singular, many string) string {
	word := many
	if quantity == 1 {
		word = singular
	}
	return fmt.Sprintf("%s %s", count(quantity), word)
}

// count groups thousands, because a six-digit message total is unreadable as a
// bare run of digits.
func count(quantity int64) string {
	digits := strconv.FormatInt(quantity, 10)
	if len(digits) <= 3 {
		return digits
	}
	var grouped []byte
	for i, digit := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, digit)
	}
	return string(grouped)
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
