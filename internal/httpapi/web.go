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
	"github.com/BrOrlandi/whatsapp-mcp/internal/brand"
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
	a := &webApp{store: store, evolution: client, sessions: newSessions(sessionKey), baseURL: strings.TrimRight(baseURL, "/"), templates: template.Must(template.New("pages").Funcs(templateFuncs).Parse(pages))}
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

// templateFuncs exposes the brand mark and small presentation helpers to the
// templates. The logo is trusted markup embedded in the binary, so it is
// inlined as template.HTML; every other value stays contextually escaped.
var templateFuncs = template.FuncMap{
	"logo":        brand.LogoSVG,
	"statusLabel": statusLabel,
	"statusTone":  statusTone,
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
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.}} · WhatsApp MCP</title><style>
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

<section class="card" aria-labelledby="instancia">
<div class="card__head"><h2 id="instancia">Instância do WhatsApp</h2>
<a class="btn btn--ghost" href="{{.BaseURL}}/manager/login" target="_blank" rel="noopener noreferrer">Abrir o Evolution Manager<span class="sr-only"> (abre em uma nova aba)</span></a></div>
<p class="muted">Conecte o QR code no Evolution Manager e volte aqui para escolher qual instância o MCP deve usar.</p>

{{if .Unavailable}}<p class="alert" role="alert">{{.Notice}} Verifique <code>EVOLUTION_URL</code> e <code>EVOLUTION_API_KEY</code> e recarregue a página.</p>
{{else if .Instances}}
<form method="post" action="/selection">
<ul class="instances">
{{range .Instances}}<li class="instance{{if eq $.Selected .ID}} instance--selected{{end}}">
<label class="instance__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if eq $.Selected .ID}} checked{{end}}>
<span class="instance__name">{{.Name}}{{if .Number}}<span class="instance__number">{{.Number}}</span>{{end}}</span>
</label>
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
{{if eq $.Selected .ID}}<span class="pill pill--current">Em uso pelo MCP</span>{{end}}
</li>{{end}}
</ul>
<div class="actions"><button class="btn" type="submit">Usar esta instância</button></div>
</form>
{{if .Selected}}<form method="post" action="/selection"><input type="hidden" name="instance_id" value=""><div class="actions"><button class="btn btn--ghost" type="submit">Remover seleção</button></div></form>{{end}}
{{else}}
<div class="empty"><p class="empty__title">Nenhuma instância encontrada</p><p class="muted">{{.Notice}}</p></div>
{{end}}
<p class="muted">O MCP usa no máximo uma instância. Selecionar outra substitui a seleção atual.</p>
</section>

<section class="card" aria-labelledby="configuracao">
<h2 id="configuracao">Configuração</h2>
<p>Evolution Go: <code>{{.BaseURL}}</code>. A autenticação usa o cabeçalho <code>apikey</code> configurado por <code>EVOLUTION_API_KEY</code>; o valor secreto nunca aparece neste painel.</p>
<p>Este binário mantém o transporte MCP via stdio. Configure seu cliente para executar o caminho absoluto do binário e forneça <code>DATABASE_URL</code>, <code>RABBITMQ_URL</code>, <code>EVOLUTION_URL</code> e <code>EVOLUTION_API_KEY</code> no ambiente seguro do cliente.</p>
<pre><code>{
  "mcpServers": {
    "whatsapp": { "command": "/caminho/absoluto/whatsapp-mcp" }
  }
}</code></pre>
<div class="link-grid"><a href="/" target="_blank" rel="noopener noreferrer">Painel do MCP</a><a href="/healthz" target="_blank" rel="noopener noreferrer">Health do MCP</a><a href="/readyz" target="_blank" rel="noopener noreferrer">Readiness do MCP</a><a href="/api/selected-instance" target="_blank" rel="noopener noreferrer">Instância selecionada (autenticado)</a><a href="{{.BaseURL}}/swagger/index.html" target="_blank" rel="noopener noreferrer">Swagger da Evolution Go</a><a href="{{.BaseURL}}/manager/login" target="_blank" rel="noopener noreferrer">Manager da Evolution Go</a></div><p class="muted">O painel MCP está publicado neste domínio. A porta interna do servidor é <code>:8080</code>; ela não precisa ser acessada diretamente. O transporte MCP continua sendo via stdio.</p>
</section>
</main></body></html>{{end}}`
