package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
)

var ErrNotFound = errors.New("not found")
var ErrAdminExists = errors.New("admin already exists")

type ControlStore interface {
	Admin(context.Context) (string, string, error)
	CreateAdmin(context.Context, string, string) error
	SelectedInstance(context.Context) (string, error)
	SelectInstance(context.Context, string) error
}
type InstanceFetcher interface {
	FetchInstances(context.Context) ([]evolution.Instance, error)
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
	evolution InstanceFetcher
	sessions  *sessions
	baseURL   string
	templates *template.Template
}

func NewWebHandler(store ControlStore, client InstanceFetcher, sessionKey []byte, baseURL string) http.Handler {
	a := &webApp{store: store, evolution: client, sessions: newSessions(sessionKey), baseURL: strings.TrimRight(baseURL, "/"), templates: template.Must(template.New("pages").Parse(pages))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.dashboard)
	mux.HandleFunc("GET /setup", a.setupPage)
	mux.HandleFunc("POST /setup", a.setup)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("POST /selection", a.selectInstance)
	mux.HandleFunc("GET /api/selected-instance", a.selectedJSON)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
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
func (a *webApp) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Erro ao renderizar página", 500)
	}
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

type dashboardData struct {
	BaseURL, Selected, Notice string
	Instances                 []evolution.Instance
	Unavailable               bool
}

func (a *webApp) dashboard(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, _ := a.store.SelectedInstance(r.Context())
	instances, err := a.evolution.FetchInstances(r.Context())
	d := dashboardData{BaseURL: a.baseURL, Selected: selected, Instances: instances, Unavailable: err != nil}
	if err != nil {
		d.Notice = "A API Evolution está indisponível no momento."
	} else if len(instances) == 0 {
		d.Notice = "Nenhuma instância encontrada. Crie uma no Evolution Manager, conecte o QR code e volte a esta página."
	}
	a.render(w, "dashboard", d)
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

const pages = `{{define "head"}}<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>WhatsApp MCP</title><style>body{margin:0;background:#f3f7f5;color:#17352b;font:16px system-ui}main{max-width:960px;margin:48px auto;padding:0 20px}.card{background:white;border:1px solid #dce8e2;border-radius:16px;padding:28px;margin:18px 0;box-shadow:0 8px 30px #17352b0d}h1,h2{color:#075e54}label{display:block;margin:14px 0 5px}input,button{font:inherit;padding:11px;border-radius:8px;border:1px solid #aac1b7}button{background:#128c7e;color:white;border:0;cursor:pointer}.muted{color:#60756e}.error{color:#a21b1b}.status{font-weight:650}code{background:#edf3f0;padding:3px 6px;border-radius:5px}pre{overflow:auto;background:#102b24;color:#e8fff7;padding:16px;border-radius:10px}form.inline{display:inline}</style></head><body><main>{{end}}
{{define "setup"}}{{template "head"}}<section class="card"><h1>Configuração inicial</h1><p>Crie o único administrador deste painel.</p>{{with .Error}}<p class="error">{{.}}</p>{{end}}<form method="post"><label>Usuário</label><input name="username" required autocomplete="username"><label>Senha (mínimo 10 caracteres)</label><input type="password" name="password" minlength="10" required autocomplete="new-password"><p><button>Criar administrador</button></p></form></section></main></body></html>{{end}}
{{define "login"}}{{template "head"}}<section class="card"><h1>Entrar</h1>{{with .Error}}<p class="error">{{.}}</p>{{end}}<form method="post"><label>Usuário</label><input name="username" required autocomplete="username"><label>Senha</label><input type="password" name="password" required autocomplete="current-password"><p><button>Entrar</button></p></form></section></main></body></html>{{end}}
{{define "dashboard"}}{{template "head"}}<header><h1>WhatsApp MCP</h1><form class="inline" method="post" action="/logout"><button>Sair</button></form></header><section class="card"><h2>Instância do WhatsApp</h2>{{if .Notice}}<p class="error">{{.Notice}}</p>{{end}}{{range .Instances}}<form method="post" action="/selection"><p><input type="radio" name="instance_id" value="{{.ID}}" {{if eq $.Selected .ID}}checked{{end}}> <strong>{{.Name}}</strong> {{if .Number}}— {{.Number}}{{end}} <span class="status">({{.Status}})</span> <button>Usar esta</button></p></form>{{end}}{{if .Selected}}<form method="post" action="/selection"><input type="hidden" name="instance_id" value=""><button>Remover seleção</button></form>{{end}}<p class="muted">O MCP usa no máximo uma instância. Selecionar outra substitui a seleção atual.</p></section><section class="card"><h2>Configuração</h2><p>Evolution Go: <code>{{.BaseURL}}</code>. A autenticação usa o cabeçalho <code>apikey</code> configurado por <code>EVOLUTION_API_KEY</code>; o valor secreto nunca aparece neste painel.</p><p>Este binário mantém o transporte MCP via stdio. Configure seu cliente para executar o caminho absoluto do binário e forneça <code>DATABASE_URL</code>, <code>RABBITMQ_URL</code>, <code>EVOLUTION_URL</code> e <code>EVOLUTION_API_KEY</code> no ambiente seguro do cliente.</p><pre>{
  "mcpServers": {
    "whatsapp": { "command": "/caminho/absoluto/whatsapp-mcp" }
  }
}</pre><p>O servidor HTTP em <code>:8080</code> oferece este painel, <code>/healthz</code>, <code>/readyz</code> e o endpoint autenticado <code>/api/selected-instance</code>. MCP por HTTP não está habilitado.</p></section></main></body></html>{{end}}`
