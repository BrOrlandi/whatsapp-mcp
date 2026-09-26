package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/BrOrlandi/whatsapp-mcp/internal/selfupdate"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

// SelfUpdater is the panel's side of the host update agent.
type SelfUpdater interface {
	Agent() (selfupdate.Agent, error)
	Available() bool
	Busy() bool
	Current() (selfupdate.Status, bool)
	Request(target, by string) (selfupdate.Request, error)
}

// Option configures the panel beyond what every deployment needs.
type Option func(*webApp)

// WithSelfUpdate lets the panel hand update requests to the host's agent.
func WithSelfUpdate(u SelfUpdater) Option {
	return func(a *webApp) { a.updater = u }
}

// updateTarget is the only version the button may ask for: the newest
// release, or — after a rollback — the version the database was already on.
// The form carries it so the page and the request agree, and it is checked
// here so a crafted form cannot name anything else.
func updateTarget() string {
	if target := newRelease(); target != "" {
		return target
	}
	return version.RolledBackFrom()
}

func (a *webApp) requestUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	if a.updater == nil {
		a.fail(w, r, "/atualizacao", "A atualização pelo painel não está disponível neste servidor.")
		return
	}
	target := updateTarget()
	if target == "" || r.FormValue("version") != target {
		a.fail(w, r, "/atualizacao", "Não há uma versão nova para instalar agora.")
		return
	}
	user, _, _ := a.admin(r)
	if _, err := a.updater.Request(target, user); err != nil {
		switch {
		case errors.Is(err, selfupdate.ErrBusy):
			http.Redirect(w, r, "/atualizacao", http.StatusSeeOther)
		case errors.Is(err, selfupdate.ErrUnavailable):
			a.fail(w, r, "/atualizacao", "Este servidor não tem o agente de atualização instalado. Use o comando por SSH.")
		default:
			a.fail(w, r, "/atualizacao", "Não foi possível pedir a atualização.")
		}
		return
	}
	http.Redirect(w, r, "/atualizacao", http.StatusSeeOther)
}

type updatePageData struct {
	layout
	Status   selfupdate.Status
	Has      bool
	Running  string
	Target   string
	Method   string
	Finished bool
}

func (a *webApp) updatePage(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	page := updatePageData{layout: a.newLayout(r, "Atualização", ""), Running: version.String()}
	if a.updater != nil {
		page.Status, page.Has = a.updater.Current()
		if agent, err := a.updater.Agent(); err == nil {
			page.Method = agent.Method
		}
	}
	page.Target = page.Status.Target
	page.Finished = page.Has && (page.Status.State == selfupdate.StateSucceeded || page.Status.State == selfupdate.StateFailed)
	a.render(w, "atualizacao", page)
}

// updateJSON is what the progress page polls. It answers without a session
// too, with nothing but the running version: the update restarts this
// process, sessions live in memory, and the page has to be able to tell that
// the new version came up even though its cookie no longer opens anything.
func (a *webApp) updateJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	body := map[string]any{"version": version.String()}
	if !a.authenticated(r) {
		body["signed_in"] = false
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	body["signed_in"] = true
	if a.updater != nil {
		if status, ok := a.updater.Current(); ok {
			body["status"] = status
		}
	}
	_ = json.NewEncoder(w).Encode(body)
}
