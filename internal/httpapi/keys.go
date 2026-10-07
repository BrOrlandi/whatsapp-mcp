package httpapi

import (
	"net/http"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
)

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
		a.fail(w, r, "/instancias", "Selecione uma instância antes de puxar o histórico.")
		return
	}
	token, err := a.store.InstanceToken(r.Context(), selected)
	if err != nil {
		a.fail(w, r, "/instancias", "Este painel não gerencia a instância selecionada.")
		return
	}
	anchor, err := a.store.OldestMessage(r.Context(), selected, "")
	if err != nil {
		a.fail(w, r, "/instancias", "Ainda não há nenhuma mensagem indexada para servir de referência. O WhatsApp só devolve mensagens anteriores a uma que ele já conhece, então aguarde as primeiras mensagens chegarem.")
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
		a.fail(w, r, "/instancias", "O WhatsApp recusou o pedido de histórico: "+err.Error())
		return
	}
	http.Redirect(w, r, "/instancias", http.StatusSeeOther)
}
