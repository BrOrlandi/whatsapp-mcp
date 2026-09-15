package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// createKey issues a credential for the selected instance and renders the
// dashboard with the secret in place.
//
// The secret is shown once, in the body of this response, and never travels
// through a redirect: a query string would land in browser history, proxy logs
// and the referrer of the next request.
func (a *webApp) createKey(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil || selected == "" {
		a.fail(w, r, "/", "Selecione uma instância antes de criar uma chave.")
		return
	}
	if _, err := a.store.InstanceToken(r.Context(), selected); err != nil {
		a.fail(w, r, "/", "Este painel não gerencia a instância selecionada, então não pode emitir chaves para ela.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "sem nome"
	}
	if len(name) > 60 {
		a.fail(w, r, "/", "Use um nome de até 60 caracteres para a chave.")
		return
	}
	secret, digest, prefix, err := store.NewAPIKey()
	if err != nil {
		a.fail(w, r, "/", "Não foi possível gerar a chave.")
		return
	}
	if err := a.store.CreateAPIKey(r.Context(), name, selected, digest, prefix); err != nil {
		a.fail(w, r, "/", "Não foi possível guardar a chave.")
		return
	}
	data := a.dashboardState(r)
	data.Endpoint = a.publicURL + "/mcp"
	data.Keys, _ = a.store.ListAPIKeys(r.Context())
	data.NewKey = secret
	a.render(w, "dashboard", data)
}

// revokeKey takes a credential out of service. The effect is immediate because
// every MCP request is authenticated on its own.
func (a *webApp) revokeKey(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		a.fail(w, r, "/", "Chave inválida.")
		return
	}
	if err := a.store.RevokeAPIKey(r.Context(), id); err != nil {
		a.fail(w, r, "/", "Não foi possível revogar a chave.")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// syncHistory asks WhatsApp for messages older than the index holds.
//
// It anchors on the oldest indexed message because that is what the protocol
// requires: WhatsApp returns the messages immediately before one it already
// knows. The messages arrive asynchronously on the history queue, so this
// returns as soon as the request is sent.
func (a *webApp) syncHistory(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil || selected == "" {
		a.fail(w, r, "/", "Selecione uma instância antes de puxar o histórico.")
		return
	}
	token, err := a.store.InstanceToken(r.Context(), selected)
	if err != nil {
		a.fail(w, r, "/", "Este painel não gerencia a instância selecionada.")
		return
	}
	anchor, err := a.store.OldestMessage(r.Context(), selected, "")
	if err != nil {
		a.fail(w, r, "/", "Ainda não há nenhuma mensagem indexada para servir de referência. O WhatsApp só devolve mensagens anteriores a uma que ele já conhece, então aguarde as primeiras mensagens chegarem.")
		return
	}
	request := evolution.Anchor{
		MessageID: anchor.MessageID,
		ChatJID:   anchor.ChatJID,
		FromMe:    anchor.FromMe,
		IsGroup:   anchor.IsGroup,
		Timestamp: anchor.SentAt,
	}
	if err := a.evolution.RequestHistory(r.Context(), token, request, 100); err != nil {
		a.fail(w, r, "/", "O WhatsApp recusou o pedido de histórico: "+err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
