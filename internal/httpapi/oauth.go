package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/ratelimit"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// OAuth uses one explicitly registered public client. PKCE authenticates code
// exchanges; exact redirect registration avoids public registration and SSRF.
// ponytail: one pre-registered client; add DCR when self-service registration is needed.
type oauthServer struct {
	store            *store.Store
	issuer, clientID string
	redirects        []string
	limit            *ratelimit.Attempts
}

// WithOAuth enables OAuth only after validating the deployment's public origin
// and callback allowlist. The operator copies callbacks from the host UI.
func WithOAuth(db *store.Store, publicURL, clientID string, redirects []string) (Option, error) {
	issuer := strings.TrimRight(publicURL, "/")
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("OAuth requires PUBLIC_URL to be an HTTPS origin without a path, query or fragment")
	}
	if clientID == "" || len(clientID) > 60 || strings.TrimSpace(clientID) != clientID || len(redirects) == 0 {
		return nil, errors.New("OAuth requires a client ID and exact redirect URIs")
	}
	for _, redirect := range redirects {
		u, err := url.Parse(redirect)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("OAuth redirect URIs must be absolute HTTPS URLs without user info or fragments")
		}
	}
	return func(a *webApp) {
		a.oauth = &oauthServer{store: db, issuer: issuer, clientID: clientID,
			redirects: slices.Clone(redirects), limit: ratelimit.New(20, time.Minute)}
	}, nil
}

func (o *oauthServer) resource() string { return o.issuer + "/mcp" }

func (o *oauthServer) mount(mux *http.ServeMux, a *webApp) {
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			oauthJSON(w, http.StatusOK, map[string]any{"resource": o.resource(), "authorization_servers": []string{o.issuer},
				"scopes_supported": []string{"whatsapp"}, "bearer_methods_supported": []string{"header"}})
		})
	}
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		oauthJSON(w, http.StatusOK, map[string]any{
			"issuer": o.issuer, "authorization_endpoint": o.issuer + "/oauth/authorize", "token_endpoint": o.issuer + "/oauth/token",
			"revocation_endpoint": o.issuer + "/oauth/revoke", "response_types_supported": []string{"code"},
			"grant_types_supported": []string{"authorization_code", "refresh_token"}, "scopes_supported": []string{"whatsapp"},
			"token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"},
			"code_challenge_methods_supported": []string{"S256"}, "authorization_response_iss_parameter_supported": true,
		})
	})
	mux.HandleFunc("GET /oauth/authorize", a.oauthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", a.oauthAuthorize)
	mux.HandleFunc("POST /oauth/token", o.token)
	mux.HandleFunc("POST /oauth/revoke", o.revoke)
}

type oauthRequest struct {
	store.OAuthAuthorization
	State string
}

// Errors before client/redirect validation never redirect to caller input.
func (o *oauthServer) authorization(query url.Values) (oauthRequest, error) {
	for _, values := range query {
		if len(values) != 1 || len(values[0]) > 2048 {
			return oauthRequest{}, errors.New("duplicate or oversized authorization parameter")
		}
	}
	request := oauthRequest{OAuthAuthorization: store.OAuthAuthorization{
		ClientID: query.Get("client_id"), RedirectURI: query.Get("redirect_uri"), Resource: query.Get("resource"), Challenge: query.Get("code_challenge")}, State: query.Get("state")}
	if request.ClientID != o.clientID || !slices.Contains(o.redirects, request.RedirectURI) {
		return oauthRequest{}, errors.New("unregistered client or redirect URI")
	}
	if request.Resource != o.resource() {
		return oauthRequest{}, errors.New("resource must identify this MCP endpoint")
	}
	if query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" {
		return oauthRequest{}, errors.New("authorization_code with S256 PKCE is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(request.Challenge)
	if err != nil || len(raw) != sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != request.Challenge {
		return oauthRequest{}, errors.New("invalid PKCE challenge")
	}
	if scope := query.Get("scope"); scope != "" && scope != "whatsapp" {
		return oauthRequest{}, errors.New("only the whatsapp scope is supported")
	}
	return request, nil
}

type oauthPage struct {
	layout
	ClientID, Callback, InstanceName, InstanceID, CSRF, Deadline string
}

func (a *webApp) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.RawQuery) > 8192 {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	request, err := a.oauth.authorization(query)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !a.authenticated(r) {
		next := "/oauth/authorize?" + query.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	must, err := a.store.AdminMustChangePassword(r.Context())
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	if must {
		http.Redirect(w, r, "/senha", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		instanceID, err := a.store.SelectedInstance(r.Context())
		if err != nil || instanceID == "" {
			http.Error(w, "Selecione uma conta de WhatsApp no painel antes de conectar.", http.StatusConflict)
			return
		}
		names, err := a.store.ManagedInstances(r.Context())
		if err != nil || names[instanceID] == "" {
			oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
			return
		}
		deadline := strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)
		a.render(w, "oauth", oauthPage{layout: layout{Title: "Conectar ao ChatGPT"}, ClientID: request.ClientID,
			Callback: request.RedirectURI, InstanceName: names[instanceID], InstanceID: instanceID, Deadline: deadline,
			CSRF: a.oauthCSRF(r, query.Encode(), instanceID, deadline)})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil || !singleValues(r.PostForm) {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	deadline := r.PostForm.Get("deadline")
	expires, err := strconv.ParseInt(deadline, 10, 64)
	instanceID := r.PostForm.Get("instance_id")
	if err != nil || time.Now().Unix() > expires || expires > time.Now().Add(5*time.Minute).Unix() ||
		!hmac.Equal([]byte(r.PostForm.Get("csrf")), []byte(a.oauthCSRF(r, query.Encode(), instanceID, deadline))) ||
		(r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.oauth.issuer) {
		oauthError(w, http.StatusForbidden, "access_denied")
		return
	}
	if r.PostForm.Get("decision") == "deny" {
		a.oauth.callback(w, r, request, "error", "access_denied")
		return
	}
	if r.PostForm.Get("decision") != "allow" {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, err := a.store.InstanceToken(r.Context(), instanceID); err != nil {
		oauthError(w, http.StatusConflict, "access_denied")
		return
	}
	request.InstanceID = instanceID
	code, err := a.oauth.store.CreateOAuthCode(r.Context(), request.OAuthAuthorization)
	if err != nil {
		a.oauth.callback(w, r, request, "error", "temporarily_unavailable")
		return
	}
	a.oauth.callback(w, r, request, "code", code)
}

func (a *webApp) oauthCSRF(r *http.Request, query, instanceID, deadline string) string {
	cookie, err := r.Cookie("whatsapp_mcp_session")
	if err != nil {
		return ""
	}
	return a.sessions.sign("oauth\x00" + cookie.Value + "\x00" + query + "\x00" + instanceID + "\x00" + deadline)
}

func (o *oauthServer) callback(w http.ResponseWriter, r *http.Request, request oauthRequest, key, value string) {
	u, _ := url.Parse(request.RedirectURI)
	query := u.Query()
	query.Set(key, value)
	query.Set("iss", o.issuer)
	if request.State != "" {
		query.Set("state", request.State)
	}
	u.RawQuery = query.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// oauthReturn accepts only this server's authorization page as a login target.
func oauthReturn(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/oauth/authorize" || u.Fragment != "" || len(value) > 8192 || u.RawPath != "" {
		return ""
	}
	return u.String()
}

func (o *oauthServer) form(w http.ResponseWriter, r *http.Request) bool {
	if !o.limit.Allow(ratelimit.ClientIP(r)) {
		w.Header().Set("Retry-After", "60")
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil || !singleValues(r.PostForm) ||
		r.PostForm.Get("client_id") != o.clientID || r.PostForm.Get("client_secret") != "" || r.Header.Get("Authorization") != "" {
		o.limit.Fail(ratelimit.ClientIP(r))
		oauthError(w, http.StatusBadRequest, "invalid_client")
		return false
	}
	return true
}

func singleValues(values url.Values) bool {
	for _, v := range values {
		if len(v) != 1 || len(v[0]) > 8192 {
			return false
		}
	}
	return true
}

func (o *oauthServer) token(w http.ResponseWriter, r *http.Request) {
	if !o.form(w, r) {
		return
	}
	form := r.PostForm
	if form.Get("resource") != o.resource() {
		oauthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	if scope := form.Get("scope"); scope != "" && scope != "whatsapp" {
		oauthError(w, http.StatusBadRequest, "invalid_scope")
		return
	}
	input := store.OAuthExchange{GrantType: form.Get("grant_type"), ClientID: o.clientID, Resource: o.resource()}
	switch input.GrantType {
	case "authorization_code":
		verifier := form.Get("code_verifier")
		if !validVerifier(verifier) || !slices.Contains(o.redirects, form.Get("redirect_uri")) {
			oauthError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		input.Credential, input.RedirectURI, input.Challenge = form.Get("code"), form.Get("redirect_uri"), base64.RawURLEncoding.EncodeToString(sum[:])
	case "refresh_token":
		input.Credential = form.Get("refresh_token")
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if input.Credential == "" || len(input.Credential) > 256 {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	token, err := o.store.ExchangeOAuthToken(r.Context(), input)
	if errors.Is(err, store.ErrOAuthGrant) {
		o.limit.Fail(ratelimit.ClientIP(r))
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	oauthJSON(w, http.StatusOK, token)
}

func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	return true
}

func (o *oauthServer) revoke(w http.ResponseWriter, r *http.Request) {
	if !o.form(w, r) {
		return
	}
	if err := o.store.RevokeOAuthToken(r.Context(), o.clientID, r.PostForm.Get("token")); err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

func oauthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	oauthJSON(w, status, map[string]string{"error": code})
}

const oauthPages = `
{{define "oauth"}}{{template "head" .}}
<div style="max-width:580px;margin:0 auto;padding-top:6vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
<section class="card"><div class="card__head"><h1>Conectar seu WhatsApp</h1></div><div class="card__body">
<p>O aplicativo <strong>{{.ClientID}}</strong> solicita acesso à conta <strong>{{.InstanceName}}</strong>.</p>
<p>Ao permitir, ele poderá ler conversas e contatos, enviar mensagens e arquivos, editar e apagar mensagens e gerenciar conversas nessa conta.</p>
<p>Você pode revogar essa conexão a qualquer momento no painel. A autorização vence em 30 dias.</p>
<p class="muted">Após sua escolha, você voltará para <code>{{.Callback}}</code>.</p>
<form method="post">
<input type="hidden" name="instance_id" value="{{.InstanceID}}">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<input type="hidden" name="deadline" value="{{.Deadline}}">
<div class="actions"><button class="btn" type="submit" name="decision" value="allow">Permitir acesso</button>
<button class="btn" type="submit" name="decision" value="deny">Cancelar</button></div>
</form></div></section></div>{{template "foot"}}{{end}}
`
