package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
	"github.com/BrOrlandi/whatsapp-mcp/internal/webhook"
)

// MediaFiles is the folder of media the tools downloaded, and the exports they
// wrote, as the settings page measures and clears them. *mcp.Server is one.
// An empty instance means every instance of the deployment.
type MediaFiles interface {
	Inventory(ctx context.Context, instanceID string) (mcp.MediaInventory, error)
	SetRetention(ctx context.Context, days int) error
	SweepMedia(ctx context.Context) (int, int64, error)
	PurgeMedia(instanceID, chat string, days int, minBytes int64, dryRun bool) (int, int64, []map[string]any, error)
	PurgeExports() (int, int64, error)
}

// WithWebhooks gives the panel the webhooks to configure. Without it the
// Webhooks card says they are unavailable on this server.
func WithWebhooks(m *webhook.Manager) Option {
	return func(a *webApp) { a.hooks = m }
}

// WithMediaFiles gives the panel the downloaded files to measure and clear.
func WithMediaFiles(m MediaFiles) Option {
	return func(a *webApp) { a.media = m }
}

// settingsPage is Configurações: how the WhatsApp MCP runs on this server.
type settingsPage struct {
	layout
	OK       string
	Endpoint string
	Admin    string
	// Transcription is the OpenAI key behind voice-note transcription.
	Transcription  bool
	Status         transcribe.Status
	Links          openAILinks
	PricePerMinute string
	Version        string
	Latest         string
	Behind         bool
	DataDir        string
	ExportsDir     string
}

func (a *webApp) settings(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	page := settingsPage{layout: a.newLayout(r, "Configurações", "configuracoes"), OK: r.URL.Query().Get("ok"), Endpoint: a.endpoint(),
		Transcription: a.transcription != nil, Links: openAI, PricePerMinute: pricePerMinute(), Version: version.String()}
	page.Admin, _, _ = a.admin(r)
	if a.transcription != nil {
		status, err := a.transcription.Status(r.Context())
		if err != nil {
			page.Error = "Não foi possível ler a configuração de transcrição."
		}
		page.Status = status
	}
	page.Latest, page.Behind = version.Latest()
	if a.media != nil {
		if inventory, err := a.media.Inventory(r.Context(), ""); err == nil {
			page.DataDir, page.ExportsDir = inventory.Dir, inventory.ExportsDir
		}
	}
	a.render(w, "configuracoes", page)
}

func pricePerMinute() string {
	return strings.Replace(strconv.FormatFloat(transcribe.PricePerMinute, 'f', -1, 64), ".", ",", 1)
}

// help is Ajuda: the questions people ask and the examples the first page
// keeps only for the first days.
func (a *webApp) help(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	page := struct {
		layout
		Prompts        []string
		PricePerMinute string
		Pricing        string
	}{a.newLayout(r, "Ajuda", "ajuda"), suggestedPrompts, pricePerMinute(), transcribe.PricingURL}
	a.render(w, "ajuda", page)
}

// apiError is a request the operator can fix, as opposed to a failure.
type apiError struct{ msg string }

func (e apiError) Error() string { return e.msg }

// ownHost reports whether a request's Origin names this panel: the host the
// request reached, the one a reverse proxy forwarded, or PUBLIC_URL's. A
// proxy that rewrites Host would otherwise make every write look foreign.
func (a *webApp) ownHost(r *http.Request, host string) bool {
	if host == "" {
		return false
	}
	if host == r.Host || host == r.Header.Get("X-Forwarded-Host") {
		return true
	}
	if public, err := url.Parse(a.publicURL); err == nil && public.Host != "" && host == public.Host {
		return true
	}
	return false
}

// api serves one JSON route of the settings page. It is behind the panel's
// session like every page, and a change is accepted only from the panel's own
// pages: the session cookie is SameSite=Lax, a JSON body cannot be posted
// across origins without a preflight this server never answers, and a browser
// that says the request came from another site is refused outright.
func (a *webApp) api(handle func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !a.authenticated(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "entre no painel de novo"})
			return
		}
		// The pages hold off everything until an installer-generated password
		// is replaced; so does the API, which can point the server at any
		// address (a webhook) or delete what it keeps. It fails closed: a
		// store that cannot answer is not a reason to let a change through.
		if must, err := a.store.AdminMustChangePassword(r.Context()); err != nil || must {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "troque a senha do painel primeiro"})
			return
		}
		if r.Method != http.MethodGet {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "pedido de outra origem"})
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if parsed, err := url.Parse(origin); err != nil || !a.ownHost(r, parsed.Host) {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "pedido de outra origem"})
					return
				}
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "mande o pedido em JSON"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		out, err := handle(r)
		if err != nil {
			var user apiError
			status := http.StatusInternalServerError
			if errors.As(err, &user) {
				status = http.StatusConflict
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

// registerSettingsAPI adds the routes the Webhooks and Arquivos baixados cards
// read and change, in the shapes the Local version's API uses.
func (a *webApp) registerSettingsAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/webhooks", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return map[string]any{"available": false, "webhooks": []webhook.Status{}, "events": webhook.Events}, nil
		}
		list, err := a.hooks.List(r.Context())
		if list == nil {
			list = []webhook.Status{}
		}
		return map[string]any{"available": true, "webhooks": list, "events": webhook.Events,
			"signature": "X-WhatsApp-MCP-Signature: sha256=<HMAC-SHA256 do corpo com o segredo do webhook, em hexadecimal>"}, err
	}))
	mux.HandleFunc("POST /api/webhooks", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return nil, apiError{"os webhooks não estão disponíveis neste servidor"}
		}
		var settings webhook.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			return nil, apiError{"pedido inválido"}
		}
		created, secret, err := a.hooks.Create(r.Context(), settings)
		if err != nil {
			return nil, webhookError(err)
		}
		status, _ := a.hooks.Get(r.Context(), created.ID)
		return map[string]any{"webhook": status, "secret": secret,
			"note": "guarde o segredo: ele não aparece de novo, e assina cada entrega no cabeçalho X-WhatsApp-MCP-Signature"}, nil
	}))
	mux.HandleFunc("GET /api/webhooks/{id}", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return nil, apiError{"os webhooks não estão disponíveis neste servidor"}
		}
		status, err := a.hooks.Get(r.Context(), r.PathValue("id"))
		return status, webhookError(err)
	}))
	mux.HandleFunc("PATCH /api/webhooks/{id}", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return nil, apiError{"os webhooks não estão disponíveis neste servidor"}
		}
		var settings webhook.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			return nil, apiError{"pedido inválido"}
		}
		if _, err := a.hooks.Update(r.Context(), r.PathValue("id"), settings); err != nil {
			return nil, webhookError(err)
		}
		status, err := a.hooks.Get(r.Context(), r.PathValue("id"))
		return status, webhookError(err)
	}))
	mux.HandleFunc("DELETE /api/webhooks/{id}", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return nil, apiError{"os webhooks não estão disponíveis neste servidor"}
		}
		if err := a.hooks.Delete(r.Context(), r.PathValue("id")); err != nil {
			return nil, webhookError(err)
		}
		return map[string]bool{"deleted": true}, nil
	}))
	mux.HandleFunc("POST /api/webhooks/{id}/test", a.api(func(r *http.Request) (any, error) {
		if a.hooks == nil {
			return nil, apiError{"os webhooks não estão disponíveis neste servidor"}
		}
		out, err := a.hooks.Test(r.Context(), r.PathValue("id"))
		return out, webhookError(err)
	}))

	mux.HandleFunc("GET /api/media", a.api(func(r *http.Request) (any, error) {
		if a.media == nil {
			return nil, apiError{"este servidor não guarda arquivos baixados"}
		}
		return a.media.Inventory(r.Context(), "")
	}))
	mux.HandleFunc("POST /api/media/retention", a.api(func(r *http.Request) (any, error) {
		if a.media == nil {
			return nil, apiError{"este servidor não guarda arquivos baixados"}
		}
		var body struct {
			Days *int `json:"days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Days == nil || *body.Days < 0 {
			return nil, apiError{`mande {"days": N}: os dias que um arquivo baixado fica guardado, ou 0 para guardar sempre`}
		}
		if err := a.media.SetRetention(r.Context(), *body.Days); err != nil {
			return nil, err
		}
		files, bytes, err := a.media.SweepMedia(r.Context())
		return map[string]any{"retention_days": *body.Days, "deleted_files": files, "freed_bytes": bytes}, err
	}))
	mux.HandleFunc("POST /api/media/purge", a.api(func(r *http.Request) (any, error) {
		if a.media == nil {
			return nil, apiError{"este servidor não guarda arquivos baixados"}
		}
		var body struct {
			What          string `json:"what"`
			OlderThanDays int    `json:"older_than_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OlderThanDays < 0 {
			return nil, apiError{`mande {"what": "media"} ou {"what": "exports"}`}
		}
		var files int
		var bytes int64
		var err error
		switch body.What {
		case "media":
			files, bytes, _, err = a.media.PurgeMedia("", "", body.OlderThanDays, 0, false)
		case "exports":
			files, bytes, err = a.media.PurgeExports()
		default:
			return nil, apiError{`what precisa ser "media" (os arquivos baixados) ou "exports" (as exportações)`}
		}
		return map[string]any{"deleted_files": files, "freed_bytes": bytes}, err
	}))
}

// webhookError turns the webhook package's errors into the API's: what the
// operator can fix is a 409, in Portuguese.
func webhookError(err error) error {
	var user webhook.UserError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &user):
		return apiError{user.Msg}
	case errors.Is(err, store.ErrNoWebhook):
		return apiError{"nenhum webhook tem esse id"}
	}
	return errors.New(strings.TrimSpace(err.Error()))
}
