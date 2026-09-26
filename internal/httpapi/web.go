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
	"log/slog"
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
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
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
	// licenseAuto registers licences with an address on licenseEmailDomain —
	// mail landing in this project's Email Worker, which clicks the magic link
	// — so activation needs no person. The email form below is the manual
	// fallback for when it is off or the worker is not reachable.
	licenseAuto        bool
	licenseEmailDomain string
	// licenseAutoWait bounds the automatic wait. autoSince remembers when this
	// process first tried, for the case where no link ever went out and there
	// is therefore no stored timestamp to measure from; a restart resets it,
	// which is the right answer because a restart is a fresh attempt.
	licenseAutoWait time.Duration
	autoMu          sync.Mutex
	autoSince       time.Time
	sessions        *sessions
	templates       *template.Template
	// logins throttles password guessing. The administrator password is the
	// weakest credential the gateway holds — a person chose it — and it opens
	// the panel that owns the WhatsApp session, so the login form needs the same
	// per-address lockout the MCP endpoint already has. bcrypt slows one guess
	// down; only a lockout stops a campaign of them.
	logins *ratelimit.Attempts
	// transcription is the OpenAI key behind voice-note transcription. Nil
	// leaves the page up with an explanation instead of a form.
	transcription TranscriptionSettings
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

func NewWebHandler(store ControlStore, client EvolutionAPI, status StatusReader, sessionKey []byte, publicURL, setupToken string, licenseAuto bool, licenseEmailDomain string, licenseAutoWait time.Duration, transcription TranscriptionSettings) http.Handler {
	if licenseAutoWait <= 0 {
		licenseAutoWait = 3 * time.Minute
	}
	a := &webApp{store: store, evolution: client, status: status, publicURL: strings.TrimRight(publicURL, "/"), setupToken: setupToken, licenseAuto: licenseAuto, licenseEmailDomain: licenseEmailDomain, licenseAutoWait: licenseAutoWait, sessions: newSessions(sessionKey), templates: template.Must(template.New("pages").Funcs(templateFuncs).Parse(pages)), logins: ratelimit.New(loginFailures, loginLockout), transcription: transcription}
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
	mux.HandleFunc("POST /instancias/licenca/auto", a.autoLicense)
	mux.HandleFunc("GET /instancias/licenca/retorno", a.completeLicense)
	mux.HandleFunc("POST /instancias/selecionar", a.selectInstance)
	mux.HandleFunc("POST /instancias/conectar", a.connectInstance)
	mux.HandleFunc("POST /instancias/desconectar", a.disconnectInstance)
	mux.HandleFunc("POST /instancias/sair", a.logoutInstance)
	mux.HandleFunc("POST /instancias/remover", a.deleteInstance)
	mux.HandleFunc("POST /instancias/historico", a.syncHistory)
	mux.HandleFunc("GET /estado", a.statusPage)
	mux.HandleFunc("GET /transcricao", a.transcriptionPage)
	mux.HandleFunc("POST /transcricao", a.saveTranscriptionKey)
	mux.HandleFunc("POST /transcricao/remover", a.removeTranscriptionKey)
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

// setSession issues the panel cookie.
//
// SameSite is Lax rather than Strict because the licensing server sends the
// operator back here by a cross-site navigation, and a Strict cookie is not
// sent on one — the operator arrived at their own panel already signed in and
// was shown the login form. Lax still refuses the cookie on cross-site POSTs,
// and every route that changes anything here is a POST, so the protection that
// mattered is intact.
func (a *webApp) setSession(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: "whatsapp_mcp_session", Value: a.sessions.create(), Path: "/", MaxAge: 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}
func (a *webApp) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("whatsapp_mcp_session"); err == nil {
		a.sessions.remove(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "whatsapp_mcp_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
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

// licensePageData is what the magic-link click renders when it arrives without
// a panel session, which is the ordinary case for a link opened from a mail
// client.
type licensePageData struct {
	layout
	OK     string
	Reason string
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
	// SelectedNumber is the WhatsApp number of the chosen instance, as
	// Evolution reports it. The pages format it before showing it.
	SelectedNumber string
	Notice         string
	Unavailable    bool
	// NeedsActivation separates the one unavailability that waiting will not
	// fix. RegisterURL is where the operator fixes it, and OperatorEmail is
	// the address already used for a licence here, offered back as prefill.
	NeedsActivation bool
	RegisterURL     string
	OperatorEmail   string
	// AutoLicense says this panel registers licences with an address of its
	// own, whose mail is clicked by the email worker — the operator's part in
	// it shrinks to watching this page.
	AutoLicense bool
	// LinkSent marks an activation link that is sitting unanswered in that
	// inbox, which is the difference between asking for an address and waiting
	// on a click.
	LinkSent bool
	// LinkSentAt is when, which is what bounds a wait that is not ending.
	LinkSentAt time.Time
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
			state.AutoLicense = a.licenseAuto
			state.Notice = "A licença ainda não foi ativada. Sem ela, a camada que mantém a sessão do WhatsApp não responde."
			if license, licenseErr := a.evolution.License(r.Context(), a.licenseCallback()); licenseErr == nil {
				state.RegisterURL = license.RegisterURL
			}
			if saved, savedErr := a.store.EvolutionLicense(r.Context()); savedErr == nil {
				state.OperatorEmail, state.LinkSentAt = saved.OperatorEmail, saved.LinkSentAt
				state.LinkSent = !saved.LinkSentAt.IsZero()
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
			state.SelectedName, state.SelectedNumber = instance.Name, instance.Number
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

// connection is one issued credential told as what it is to the person who
// issued it: an AI tool wired to their WhatsApp. The key behind it is an
// implementation detail the page mentions only in passing, because "revoke the
// key wamcp-a1b2c3" is not a sentence anybody wants to reason about.
type connection struct {
	store.APIKey
	// Tool is what to call this connection: the name the tool gave itself in
	// the MCP handshake when there is one, and otherwise whatever the operator
	// chose when creating it.
	Tool string
	// Detected marks a Tool that the tool itself reported, as opposed to a
	// label typed in this panel. Only the first is evidence of anything.
	Detected bool
	// Live means a client has authenticated with this credential at least once,
	// which is the only proof the panel has that a connection actually works.
	Live bool
}

// connectPage is the landing page: the state of the WhatsApp line, the AI tools
// connected to it, and the one button that adds another.
type connectPage struct {
	layout
	Ready        bool
	NeedsPairing bool
	// NeedsActivation carries the one blocker this page cannot resolve on its
	// own, so it can point at the wizard that can instead of only naming it.
	NeedsActivation bool
	Notice          string
	InstanceName    string
	// Phone is the connected line, written the way its owner writes it.
	// Account is the profile name WhatsApp reports for it.
	Phone    string
	Account  string
	Endpoint string
	// Connections are the live credentials, presented as tools rather than
	// keys. Keys keeps the raw rows for the parts of the page that still count
	// them.
	Connections []connection
	Keys        []store.APIKey
	// HasKey and ClientConnected are facts the panel already holds rather than
	// a stored notion of progress: a key exists or it does not, and a key that
	// has been used proves a client authenticated with it. Disconnecting the
	// last tool therefore takes the page back to its empty state on its own.
	HasKey          bool
	ClientConnected bool
	// LiveCount is how many connections have actually been used, which is the
	// number worth putting on the page: a credential nobody has presented is a
	// connection that does not exist yet.
	LiveCount int64
	LastUse   time.Time
	Setup     clientSetup
	Prompts   []string
	// Clients is the list of setup routes the "new connection" dialog offers.
	Clients []clientOption
}

// clientOption is one choice in the dialog that starts a connection.
type clientOption struct {
	Value, Label, Hint string
	First              bool
}

// clientOptions describes each setup route in the words of someone who has
// never heard of MCP.
func clientOptions() []clientOption {
	hints := map[string]string{
		"desktop": "O aplicativo do Claude no computador. É o caminho mais simples: copiar, colar e reiniciar.",
		"code":    "O Claude que roda no terminal. Um comando só.",
		"outros":  "Cursor, ChatGPT, Windsurf, n8n… Geramos um texto pronto para você colar no seu assistente, e ele mesmo se configura.",
	}
	options := make([]clientOption, 0, len(clients))
	for index, client := range clients {
		options = append(options, clientOption{Value: client.Value, Label: client.Label, Hint: hints[client.Value], First: index == 0})
	}
	return options
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
		Phone:           phone(state.SelectedNumber),
		Endpoint:        a.endpoint(),
	}
	if a.status != nil {
		page.Account = a.status.Snapshot().WhatsApp.PushName
	}
	page.Keys = keys
	page.Setup = newClientSetup(page.Endpoint, "")
	page.Prompts = suggestedPrompts
	page.Clients = clientOptions()
	page.HasKey = len(page.Keys) > 0
	for _, key := range page.Keys {
		if key.LastUsedAt.After(page.LastUse) {
			page.LastUse, page.ClientConnected = key.LastUsedAt, true
		}
		view := connection{APIKey: key, Tool: key.Name, Live: !key.LastUsedAt.IsZero()}
		if reported := toolLabel(key.ClientName); reported != "" {
			view.Tool, view.Detected = reported, true
		}
		if view.Live {
			page.LiveCount++
		}
		page.Connections = append(page.Connections, view)
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
	// Auto means the registration went to an address this project's email
	// worker reads — the wait is on a machine, not on a person's inbox.
	Auto bool
	// AutoStalled means that wait has gone on long past the seconds a working
	// worker takes, so the wizard stops promising and offers the manual flow.
	AutoStalled bool

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
		// Whether this is the automatic flow is a property of the pending
		// registration, not of the setting: once the operator has fallen back
		// to their own address, the wizard has to talk about their inbox even
		// though EVOLUTION_LICENSE_AUTO is still on.
		switch {
		case a.licenseAuto && (!page.Sent || autoAddress(page.OperatorEmail)):
			// The automatic mode registers with an address of this
			// deployment's own, whose mail is clicked by the email worker —
			// nobody reads inboxes here. The first render asks for it, and
			// every render after that is polling on the licence coming in.
			page.Auto = true
			if !page.Sent {
				a.markAutoAttempt()
				if a.startAutoLicense(r, &page.OperatorEmail) {
					page.Sent = true
				}
			}
			// The click a working worker performs lands in seconds. Past the
			// wait, something between here and that mailbox is broken and no
			// amount of further waiting fixes it, so the operator gets their
			// own inbox back — which is a flow that needs nothing of ours to
			// be working. The poll stays armed either way: a late click still
			// moves the page on.
			page.AutoStalled = a.autoStalled(state)
			// Once the link is out, the wizard's own poll (app.js, via
			// /api/instalacao) watches for the activation; before that, this
			// page is the retry loop — usually waiting for Evolution to
			// accept a registration at all.
			if !page.Sent && !page.AutoStalled {
				page.Refresh = true
			}
		case !page.Sent && page.OperatorEmail != "" && a.startLicense(r, page.OperatorEmail):
			// The address was typed when the administrator account was created, so
			// there is nothing left to ask: send the link and let the operator
			// arrive at their inbox instead of at one more form.
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
	// The manual copy names the address it is waiting on, which is right when
	// that address is the operator's and wrong when it is one of ours. A
	// deployment that sent automatically and then had EVOLUTION_LICENSE_AUTO
	// turned off would otherwise show the maintainer's domain in a sentence
	// addressed to the operator.
	if !page.Auto && autoAddress(page.OperatorEmail) {
		page.OperatorEmail = ""
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

// startAutoLicense registers the licence with an address whose mail is read by
// this project's email worker, which clicks the link — activation with no
// inbox and no operator. The address is kept across renders through the same
// slot the manual one uses, so a wait that spans reloads does not mint a new
// registration every five seconds.
func (a *webApp) startAutoLicense(r *http.Request, kept *string) bool {
	email := *kept
	if !autoAddress(email) {
		generated, err := a.licenseEmailAddress()
		if err != nil {
			return false
		}
		email = generated
	}
	if a.startLicense(r, email) {
		*kept = email
		return true
	}
	return false
}

// autoLocalPart is the local part every automatically registered address
// carries, and therefore how a pending registration is told apart from one
// waiting on a person's inbox.
const autoLocalPart = "whatsappmcp+"

// autoAddress reports whether a pending registration belongs to this
// deployment's own machine-read mailbox rather than to somebody's inbox.
func autoAddress(email string) bool { return strings.HasPrefix(email, autoLocalPart) }

// markAutoAttempt starts the clock on the automatic path the first time this
// process tries it. It is only consulted when no link ever went out, since a
// link that did leaves a timestamp in the database that outlives a restart.
func (a *webApp) markAutoAttempt() {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoSince.IsZero() {
		a.autoSince = time.Now()
	}
}

// autoStalled reports whether the automatic path has been given its chance and
// has not delivered. Both failures it covers look the same to the operator and
// have the same answer — take over with your own inbox:
//
//   - the link went out and nothing clicked it, measured from the stored
//     timestamp so the verdict survives a restart mid-wait;
//   - no link ever went out, because Evolution or the licensing server keeps
//     refusing the registration, measured from this process's first attempt.
func (a *webApp) autoStalled(state selection) bool {
	if !state.LinkSentAt.IsZero() {
		return time.Since(state.LinkSentAt) > a.licenseAutoWait
	}
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return !a.autoSince.IsZero() && time.Since(a.autoSince) > a.licenseAutoWait
}

// licenseEmailAddress mints the address automatic licences register under. The
// plus-addressed local part is deliberate: Cloudflare Email Routing has a
// single exact rule for the `whatsappmcp` base that routes everything with a
// `+<detail>` to the licence worker — no catch-all, no other mail involved —
// and the random detail keeps each installation its own registration.
func (a *webApp) licenseEmailAddress() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%x@%s", autoLocalPart, raw, a.licenseEmailDomain), nil
}

// autoLicense is the button behind both wizard and panel: it asks for an
// automatic registration wherever the ask came from.
func (a *webApp) autoLicense(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	var kept string
	if saved, err := a.store.EvolutionLicense(r.Context()); err == nil {
		kept = saved.OperatorEmail
	}
	if !a.startAutoLicense(r, &kept) {
		a.fail(w, r, origin(r), "Não foi possível pedir a ativação automática ao servidor de licenças. Tente de novo em instantes.")
		return
	}
	http.Redirect(w, r, origin(r), http.StatusSeeOther)
}

// completeLicense finishes the registration the magic link started. The
// licensing server sends the operator's browser here with a one-time code that
// only it can judge, which makes the code the proof of identity the same way
// the installer's setup token is: it is accepted without a panel session, and
// it stops meaning anything the moment it is spent.
func (a *webApp) completeLicense(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.licenseOutcome(w, r, "", "O link de ativação veio sem o código. Peça outro no painel.")
		return
	}
	activation, err := a.evolution.CompleteActivation(r.Context(), code)
	if err != nil {
		// This is the one failure nobody is watching. The click can arrive in
		// a mail client's own browser, or from the licence worker, and the
		// only place the reason went was a page rendered back to whoever made
		// the request — so a refused activation left no trace on the server at
		// all. It took reading a Cloudflare worker's logs to find out why one
		// was failing. Now it is in the gateway's log.
		slog.Error("licence activation refused", "error", err.Error())
		a.licenseOutcome(w, r, "", "Não foi possível ativar a licença: "+err.Error())
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
		a.licenseOutcome(w, r, "A licença foi ativada, mas este painel não conseguiu guardar uma cópia dela para reativar rebuilds futuros.", "")
		return
	}
	a.licenseOutcome(w, r, "Licença ativada.", "")
}

// licenseOutcome reports how the magic-link click went.
//
// The click can land in any browser: mail clients open links in their own
// in-app one, where this panel has no session at all. Sending that visitor to
// a page that requires one lost them on a login form and threw the reason
// away with the redirect — which is how a failed activation came to look like
// a wizard that simply never finished. So the answer is a page of its own,
// readable without a session, and the return into the wizard only happens for
// the browser that already has one.
func (a *webApp) licenseOutcome(w http.ResponseWriter, r *http.Request, ok, reason string) {
	if a.authenticated(r) {
		if reason != "" {
			a.fail(w, r, "/instalacao", reason)
			return
		}
		http.Redirect(w, r, "/instalacao?ok="+url.QueryEscape(ok), http.StatusSeeOther)
		return
	}
	page := licensePageData{layout: layout{Title: "Ativação da licença"}, OK: ok, Reason: reason}
	if reason != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	a.render(w, "licenca", page)
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

// updateCommand is what the panel hands an operator whose instance is behind:
// the same one-liner the documentation gives, so there is one command to get
// wrong rather than two. It is a constant because it names a script in this
// repository, not anything about this installation.
const updateCommand = "curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash"

// newRelease is the version to update to, or empty when there is nothing to
// say — no check has succeeded, the check is off, this build is already
// current, or it is ahead of every release. Empty is what the banner tests, so
// every one of those cases renders nothing at all.
func newRelease() string {
	if latest, behind := version.Latest(); behind {
		return latest
	}
	return ""
}

// releaseURL points at the notes for a version, which is the only place that
// answers "is this worth updating for".
func releaseURL(tag string) string {
	return brand.RepositoryURL + "/releases/tag/v" + tag
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
	"version":       version.String,
	"newRelease":    newRelease,
	"releaseURL":    releaseURL,
	"updateCommand": func() string { return updateCommand },
	"statusLabel":   statusLabel,
	"statusTone":    statusTone,
	"sessionLabel":  sessionLabel,
	"sessionTone":   sessionTone,
	"moment":        moment,
	"relativeSince": relativeSince,
	"plural":        plural,
	"count":         count,
	"len64":         len64,
	"phone":         phone,
	"initial":       initial,
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
