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
	SaveOperatorEmail(context.Context, string) error
	MarkLicenseLinkSent(context.Context, string) error
	SaveEvolutionLicense(context.Context, store.EvolutionLicense) error
	EvolutionLicense(context.Context) (store.EvolutionLicense, error)
}

// EvolutionAPI is the slice of Evolution this panel drives. The panel owns the
// whole instance lifecycle so the operator never needs the Evolution Manager,
// which is why creation, pairing and teardown all appear here. It also owns
// the whole licence lifecycle, for the same reason.
type EvolutionAPI interface {
	FetchInstances(context.Context) ([]evolution.Instance, error)
	CreateInstance(context.Context, string, string) (evolution.Instance, error)
	DeleteInstance(context.Context, string) error
	ConnectInstance(context.Context, string) error
	DisconnectInstance(context.Context, string) error
	LogoutInstance(context.Context, string) error
	QRCode(context.Context, string) (evolution.QRCode, error)
	License(context.Context, string) (evolution.License, error)
	RegisterOperator(context.Context, string, string, string) error
	CompleteActivation(context.Context, string) (evolution.LicenseActivation, error)
	ReactivateLicense(context.Context, string) error
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

// minPassword is the shortest administrator password the panel accepts. It is
// short on purpose: what actually stops guessing here is the per-address
// lockout on the login form, which gives up after loginFailures attempts, and
// a length rule long enough to be annoying mostly buys passwords written on
// paper. The ceiling is bcrypt's own — it silently truncates past 72 bytes.
const (
	minPassword = 6
	maxPassword = 72
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
	mux.HandleFunc("GET /instalacao", a.onboarding)
	mux.HandleFunc("GET /instancias", a.instances)
	mux.HandleFunc("POST /instancias", a.createInstance)
	mux.HandleFunc("POST /instancias/licenca", a.sendLicenseLink)
	mux.HandleFunc("GET /instancias/licenca/retorno", a.completeLicense)
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
	mux.HandleFunc("GET /api/instalacao", a.onboardingJSON)
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
	if len(next) < minPassword || len([]byte(next)) > maxPassword {
		page := a.passwordLayout(r)
		page.Error = "Use uma senha com pelo menos 6 caracteres."
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
	// The administrator is identified by their email rather than by a name they
	// invent. It is one field instead of two, it is the credential people
	// already expect to sign in with, and it is the address the next step has
	// to reach — which is why it has to be one the operator can actually open.
	email := strings.TrimSpace(r.FormValue("email"))
	p := r.FormValue("password")
	if !strings.Contains(email, "@") || len(email) > 254 {
		a.render(w, "setup", setupPageData{layout: layout{Title: "Configuração inicial", Error: "Informe um e-mail válido. Você vai precisar confirmá-lo no passo seguinte."}, Token: a.setupToken, Email: email})
		return
	}
	if len(p) < minPassword || len([]byte(p)) > maxPassword {
		a.render(w, "setup", setupPageData{layout: layout{Title: "Configuração inicial", Error: "Use uma senha com pelo menos 6 caracteres."}, Token: a.setupToken, Email: email})
		return
	}
	h, err := auth.HashPassword(p)
	if err != nil {
		http.Error(w, "Erro interno", 500)
		return
	}
	if err = a.store.CreateAdmin(r.Context(), email, h); err != nil {
		http.Error(w, "O administrador já existe.", 409)
		return
	}
	// Remembering the address is all that happens here. Asking Evolution for
	// the link is the wizard's job, because Evolution is usually still coming
	// up at this exact moment.
	_ = a.store.SaveOperatorEmail(r.Context(), email)
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
	// Email is given back after a rejected submission, so a typo in the
	// password does not cost the operator their address as well.
	Email string
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
	// NeedsActivation separates the one unavailability that waiting will not
	// fix. RegisterURL is where the operator fixes it, and OperatorEmail is
	// the address already used for a licence here, offered back as prefill.
	NeedsActivation bool
	RegisterURL     string
	OperatorEmail   string
	// LinkSent marks an activation link that is sitting unanswered in that
	// inbox, which is the difference between asking for an address and waiting
	// on a click.
	LinkSent bool
	// LicenseHealed marks the render where a licence the panel was already
	// holding was put back into Evolution without anyone being asked for it.
	LicenseHealed bool
	Ready         bool
	NeedsPairing  bool
}

// licenseCallback is where the licensing server sends the operator after the
// magic-link click. It has to be the public address, since it is the
// operator's browser doing the walking.
func (a *webApp) licenseCallback() string {
	return a.publicURL + "/instancias/licenca/retorno"
}

// readSelection lists the instances and works out which one the MCP is using.
// Adoption happens here: Evolution's listing carries each instance's token, so
// an instance created before this panel existed can be taken over instead of
// paired again.
func (a *webApp) readSelection(r *http.Request) selection {
	selected, _ := a.store.SelectedInstance(r.Context())
	managed, _ := a.store.ManagedInstances(r.Context())
	instances, err := a.evolution.FetchInstances(r.Context())
	if err != nil {
		state := selection{Selected: selected, Unavailable: true, Notice: "A API do WhatsApp está indisponível no momento. Tente novamente em instantes."}
		// "Try again in a moment" is the wrong thing to say when nothing is
		// going to change on its own. An Evolution without a licence answers
		// 503 on every route until somebody registers it, so say that, and
		// carry the link that does it.
		if errors.Is(err, evolution.ErrNotActivated) {
			// A licence this panel already holds can be handed back without
			// asking anyone anything — Evolution losing its database volume to
			// a rebuild is exactly the case this covers — so try that first.
			// Only when there is no licence of ours does this become a form.
			if a.reactivateSavedLicense(r) {
				if instances, err = a.evolution.FetchInstances(r.Context()); err == nil {
					return a.listedSelection(r, selected, managed, instances, selection{Selected: selected, LicenseHealed: true})
				}
				// The licence is back in, so whatever this failure is, it is
				// no longer a licence problem — report it as a plain outage.
				return state
			}
			state.NeedsActivation = true
			state.Notice = "A licença ainda não foi ativada. Sem ela, a camada que mantém a sessão do WhatsApp não responde."
			if license, licenseErr := a.evolution.License(r.Context(), a.licenseCallback()); licenseErr == nil {
				state.RegisterURL = license.RegisterURL
			}
			if saved, savedErr := a.store.EvolutionLicense(r.Context()); savedErr == nil {
				state.OperatorEmail, state.LinkSent = saved.OperatorEmail, !saved.LinkSentAt.IsZero()
			}
		}
		return state
	}
	return a.listedSelection(r, selected, managed, instances, selection{Selected: selected})
}

// listedSelection builds a selection out of an instance listing that answered.
func (a *webApp) listedSelection(r *http.Request, selected string, managed map[string]string, instances []evolution.Instance, state selection) selection {
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

// reactivateSavedLicense hands Evolution the licence credential the panel kept
// from a previous registration. It reports whether Evolution came back to life,
// because a key the licensing server has rejected will keep failing, and the
// operator should then be offered a fresh registration instead of a loop.
func (a *webApp) reactivateSavedLicense(r *http.Request) bool {
	saved, err := a.store.EvolutionLicense(r.Context())
	if err != nil || saved.APIKey == "" {
		return false
	}
	return a.evolution.ReactivateLicense(r.Context(), saved.APIKey) == nil
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
	// NeedsActivation carries the one blocker this page cannot resolve on its
	// own, so it can point at the wizard that can instead of only naming it.
	NeedsActivation bool
	Notice          string
	InstanceName    string
	Endpoint        string
	Keys            []store.APIKey
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
	keys, _ := a.store.ListAPIKeys(r.Context())
	// A deployment that has never worked does not need a panel; it needs the
	// one next step. The wizard is that, and it is where this page's own
	// "cannot do anything yet" card used to send people anyway.
	if firstRun(state, len(keys)) {
		http.Redirect(w, r, "/instalacao", http.StatusSeeOther)
		return
	}
	page := connectPage{
		layout:          a.newLayout(r, "Conectar", "conectar"),
		Ready:           state.Ready,
		NeedsPairing:    state.NeedsPairing,
		NeedsActivation: state.NeedsActivation,
		Notice:          state.Notice,
		InstanceName:    state.SelectedName,
		Endpoint:        a.endpoint(),
	}
	page.Keys = keys
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

// instancesPage manages the WhatsApp accounts themselves. OK is a read-once
// confirmation, like the inverse of the layout's Error.
type instancesPage struct {
	layout
	selection
	OK string
}

func (a *webApp) instances(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	a.render(w, "instancias", instancesPage{layout: a.newLayout(r, "Instâncias", "instancias"), selection: a.readSelection(r), OK: r.URL.Query().Get("ok")})
}

// wizardStep is one dot in the installation stepper.
type wizardStep struct {
	Number int
	Label  string
	// State is done, now or next, and is the only thing the stylesheet reads,
	// so the class attribute never carries an unbounded value.
	State string
}

// onboardingPage is the first run, and nothing else. A deployment that has
// just been installed has no licence and no paired phone: that is the normal
// starting state, not a fault, and the panel used to greet its owner with the
// same red banners it uses for an outage. The wizard says one thing at a time,
// carries no tab bar to wander off into, and hands over to the panel as soon
// as the gateway can actually do something.
type onboardingPage struct {
	layout
	// Step is the open step: 1 licence, 2 WhatsApp, 3 done.
	Step  int
	Steps []wizardStep
	OK    string

	// Sent means the licensing server accepted a registration for
	// OperatorEmail, so this step is a wait on an inbox rather than a form.
	Sent          bool
	OperatorEmail string
	RegisterURL   string

	Instances    []instanceView
	NeedsPairing bool
	QRCode       template.URL
	QRNotice     string
	Unavailable  bool
	Notice       string
}

func (a *webApp) onboarding(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	a.render(w, "instalacao", a.onboardingState(r))
}

// onboardingState works out which step is open from what the gateway reports,
// not from a stored notion of progress: a licence that was revoked reopens the
// first step on its own, and a phone that was unlinked reopens the second.
func (a *webApp) onboardingState(r *http.Request) onboardingPage {
	page := onboardingPage{layout: layout{Title: "Instalação", Error: r.URL.Query().Get("erro")}, OK: r.URL.Query().Get("ok")}
	state := a.readSelection(r)
	switch {
	case state.NeedsActivation:
		page.Step = 1
		page.OperatorEmail, page.RegisterURL = state.OperatorEmail, state.RegisterURL
		page.Sent = state.LinkSent
		// The address was typed when the administrator account was created, so
		// there is nothing left to ask: send the link and let the operator
		// arrive at their inbox instead of at one more form.
		if !page.Sent && page.OperatorEmail != "" && a.startLicense(r, page.OperatorEmail) {
			page.Sent = true
		}
	case state.Unavailable:
		// Not a licence problem, so not something this wizard can fix. Say so
		// plainly and keep looking, rather than offering a form that will fail.
		page.Step, page.Unavailable, page.Notice, page.Refresh = 2, true, state.Notice, true
	case state.Ready:
		page.Step = 3
	default:
		page.Step, page.Instances, page.NeedsPairing = 2, state.Instances, state.NeedsPairing
		if state.NeedsPairing {
			page.Refresh = true
			page.QRCode, page.QRNotice = a.pairingCode(r)
		}
	}
	page.Steps = wizardSteps(page.Step)
	return page
}

// wizardSteps labels the stepper. The third step is a hand-off rather than a
// screen: connecting a client is the panel's own page, and a second copy of
// that checklist here would be one more thing to keep in sync.
func wizardSteps(open int) []wizardStep {
	labels := []string{"Licença", "WhatsApp", "Cliente"}
	steps := make([]wizardStep, 0, len(labels))
	for i, label := range labels {
		step := wizardStep{Number: i + 1, Label: label, State: "next"}
		switch {
		case i+1 < open:
			step.State = "done"
		case i+1 == open:
			step.State = "now"
		}
		steps = append(steps, step)
	}
	return steps
}

// firstRun reports whether this deployment has never been finished, which is
// the only time the panel hands its owner the wizard instead of itself. An
// issued key is proof the installation once worked, so a later outage lands on
// the panel — where the state page and the instance controls are — instead of
// on an install screen that cannot help with it.
func firstRun(state selection, keys int) bool {
	if keys > 0 {
		return false
	}
	if state.NeedsActivation {
		return true
	}
	if state.Unavailable {
		return false
	}
	return !state.Ready
}

// onboardingJSON is what the wizard polls while it waits on the one thing no
// automation can do: the operator clicking a link in their own inbox. It
// answers with the step that is open and nothing else, so the page can move on
// by itself instead of asking for a reload nobody knows to perform.
func (a *webApp) onboardingJSON(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	step := 2
	instances, err := a.evolution.FetchInstances(r.Context())
	switch {
	case errors.Is(err, evolution.ErrNotActivated):
		step = 1
	case err != nil:
		// An outage is not progress; leave the wizard where it is.
	default:
		selected, _ := a.store.SelectedInstance(r.Context())
		for _, instance := range instances {
			if instance.ID == selected && instance.Status == evolution.StatusConnected {
				step = 3
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"step": step})
}

// origin is the page a form was submitted from. The wizard reuses the panel's
// own handlers rather than growing copies of them, so each form says where it
// came from and every handler stays a single path.
func origin(r *http.Request) string {
	if r.FormValue("origem") == "instalacao" {
		return "/instalacao"
	}
	return "/instancias"
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
	back := origin(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 60 {
		a.fail(w, r, back, "Informe um nome de instância com até 60 caracteres.")
		return
	}
	token, err := newInstanceToken()
	if err != nil {
		a.fail(w, r, back, "Não foi possível gerar as credenciais da instância.")
		return
	}
	created, err := a.evolution.CreateInstance(r.Context(), name, token)
	if err != nil {
		if errors.Is(err, evolution.ErrNotActivated) {
			a.fail(w, r, back, "A licença ainda não foi ativada. Ative-a e tente de novo.")
			return
		}
		a.fail(w, r, back, "O WhatsApp recusou a criação da instância: "+err.Error())
		return
	}
	if err := a.store.SaveInstance(r.Context(), created.ID, created.Name, token); err != nil {
		a.fail(w, r, back, "A instância foi criada, mas não foi possível guardar suas credenciais.")
		return
	}
	if err := a.store.SelectInstance(r.Context(), created.ID); err != nil {
		a.fail(w, r, back, "A instância foi criada, mas não foi possível selecioná-la.")
		return
	}
	if err := a.evolution.ConnectInstance(r.Context(), token); err != nil {
		a.fail(w, r, back, "A instância foi criada, mas não foi possível iniciá-la: "+err.Error())
		return
	}
	// The wizard shows the QR code on its own second step; the panel has a
	// page for it.
	if back == "/instalacao" {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/pair", http.StatusSeeOther)
}

func (a *webApp) selectInstance(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	back := origin(r)
	id := r.FormValue("instance_id")
	instances, err := a.evolution.FetchInstances(r.Context())
	if err != nil {
		if errors.Is(err, evolution.ErrNotActivated) {
			a.fail(w, r, back, "A licença ainda não foi ativada; ative-a antes de escolher uma instância.")
			return
		}
		a.fail(w, r, back, "A API do WhatsApp está indisponível no momento.")
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
		a.fail(w, r, back, "Não foi possível salvar a seleção.")
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
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
	page.QRCode, page.Notice = a.pairingCode(r)
	a.render(w, "pair", page)
}

// pairingCode asks Evolution for the QR of the selected instance, and says why
// there is none when there is none. Both the pairing page and the installation
// wizard show the same code, so the reasons are worded once.
func (a *webApp) pairingCode(r *http.Request) (template.URL, string) {
	const notReady = "O QR code ainda não está pronto. Esta página tenta de novo sozinha."
	token, err := a.selectedToken(r)
	if err != nil {
		return "", "Nenhuma instância deste painel está selecionada."
	}
	code, err := a.evolution.QRCode(r.Context(), token)
	switch {
	case errors.Is(err, evolution.ErrLoggedIn):
		return "", "A sessão já está pareada. Aguardando a conexão ficar ativa."
	case err != nil:
		return "", notReady
	}
	image := qrImageSource(code.Image)
	if image == "" {
		return "", notReady
	}
	return image, ""
}

func (a *webApp) connectInstance(w http.ResponseWriter, r *http.Request) {
	destination := "/pair"
	if origin(r) == "/instalacao" {
		destination = "/instalacao"
	}
	a.instanceAction(w, r, destination, func(ctx context.Context, token string) error {
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
		a.fail(w, r, origin(r), "Selecione uma instância deste painel antes desta ação.")
		return
	}
	if err := run(r.Context(), token); err != nil {
		a.fail(w, r, origin(r), "O WhatsApp recusou a operação: "+err.Error())
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

// sendLicenseLink starts a licence registration for the operator who filled the
// form. The email is the operator's own; the licensing server will send a
// magic link to it, and only the person who can read that inbox can click it.
// That click is the identity the licence is issued for, so this is as far as
// any automation can go without pretending to be somebody.
func (a *webApp) sendLicenseLink(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if !strings.Contains(email, "@") || len(email) > 254 {
		a.fail(w, r, "/instalacao", "Informe um e-mail válido para ativar.")
		return
	}
	// The licensing server's registry wants a {token, email, name}, but the
	// identity is the email — the name is a label, so the product supplies
	// its own and the operator has one field to type. A person who prefers
	// their own name on the registry can still use Evolution's own link below.
	if err := a.evolution.RegisterOperator(r.Context(), email, brand.Name, a.licenseCallback()); err != nil {
		a.fail(w, r, "/instalacao", "Não foi possível enviar o e-mail de ativação: "+err.Error())
		return
	}
	if err := a.store.MarkLicenseLinkSent(r.Context(), email); err != nil {
		// The link is already on its way; losing the prefill is not worth
		// telling the operator about, but it is worth noticing in logs one day.
		_ = err
	}
	// The wizard's own copy names the address and the deadline, so the banner
	// only has to confirm that this click did something — which is what the
	// operator needs when they asked for the link a second time.
	http.Redirect(w, r, "/instalacao?ok="+url.QueryEscape("Link de ativação enviado."), http.StatusSeeOther)
}

// startLicense puts an activation link in an inbox the panel already knows
// about, and reports whether one is now waiting there.
//
// It runs from the wizard's own render rather than from the form that created
// the administrator, because right after an install Evolution is usually still
// booting and cannot mint a registration token yet. Doing it here means the
// link goes out on the first render that finds Evolution awake and unlicensed,
// and the stored timestamp is what keeps it to one link per wait.
func (a *webApp) startLicense(r *http.Request, email string) bool {
	if err := a.evolution.RegisterOperator(r.Context(), email, brand.Name, a.licenseCallback()); err != nil {
		return false
	}
	return a.store.MarkLicenseLinkSent(r.Context(), email) == nil
}

// completeLicense finishes the registration the magic link started. The
// licensing server sends the operator's browser here with a one-time code that
// only it can judge, which makes the code the proof of identity the same way
// the installer's setup token is: it is accepted without a panel session, and
// it stops meaning anything the moment it is spent.
func (a *webApp) completeLicense(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.fail(w, r, "/instalacao", "O link de ativação veio sem o código. Peça o e-mail de novo.")
		return
	}
	activation, err := a.evolution.CompleteActivation(r.Context(), code)
	if err != nil {
		a.fail(w, r, "/instalacao", "Não foi possível ativar a licença: "+err.Error())
		return
	}
	if err := a.store.SaveEvolutionLicense(r.Context(), store.EvolutionLicense{
		InstanceID: activation.InstanceID,
		APIKey:     activation.APIKey,
		Tier:       activation.Tier,
		CustomerID: activation.CustomerID,
	}); err != nil {
		// Evolution is alive; only the panel's copy of the credential failed to
		// persist. The deployment works — say so, losing only rebuild comfort.
		a.fail(w, r, "/instalacao", "A licença foi ativada, mas este painel não conseguiu guardar uma cópia dela para rebuilds futuros.")
		return
	}
	http.Redirect(w, r, "/instalacao?ok="+url.QueryEscape("Licença ativada."), http.StatusSeeOther)
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
	"product":       func() string { return brand.Name },
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
