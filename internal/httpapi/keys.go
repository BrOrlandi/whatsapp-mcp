package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// keyPage shows a credential exactly once, together with everything needed to
// use it. Handing over a bare secret and leaving the operator to assemble the
// client configuration is where the flow used to break, so the snippets arrive
// already filled in.
type keyPage struct {
	layout
	Name   string
	Secret string
	Setup  clientSetup
	Prompt string
}

// clientSetup is everything a client needs, rendered the same way wherever it
// appears. On the page that creates a key it carries the real secret; anywhere
// else it carries a placeholder, because the secret is shown once and only once.
type clientSetup struct {
	Endpoint   string
	Secret     string
	HasSecret  bool
	Command    string
	CommandEnv string
	JSON       string
	// AgentPrompt is the escape hatch for every client this panel has no
	// screenshot of: instead of instructions the operator has to translate, it
	// is a message they paste into the assistant itself, which then configures
	// its own MCP connection or explains how.
	AgentPrompt string
	// Preferred is the tab that opens first, named by the operator's own answer
	// to "where are you going to use this?". Landing on the wrong client's
	// instructions is the moment the flow loses people.
	Preferred string
}

// keyPlaceholder stands in for the secret once it can no longer be shown.
const keyPlaceholder = "SUA_CHAVE"

// clients are the setup routes the panel offers, in the order it offers them.
// Claude Desktop leads because it is the one that needs no terminal.
var clients = []struct{ Value, Label string }{
	{"desktop", "Claude Desktop"},
	{"code", "Claude Code"},
	{"outros", "Outra ferramenta de IA"},
}

// clientChoice resolves the form value to a tab, falling back to the first
// route rather than to an empty page.
func clientChoice(value string) string {
	for _, client := range clients {
		if client.Value == value {
			return client.Value
		}
	}
	return clients[0].Value
}

// clientLabel names a chosen route, for use as the connection's own name.
func clientLabel(value string) string {
	for _, client := range clients {
		if client.Value == value {
			return client.Label
		}
	}
	return clients[0].Label
}

func newClientSetup(endpoint, secret string) clientSetup {
	return newClientSetupFor(endpoint, secret, clients[0].Value)
}

func newClientSetupFor(endpoint, secret, preferred string) clientSetup {
	setup := clientSetup{Endpoint: endpoint, Secret: secret, HasSecret: secret != "", Preferred: clientChoice(preferred)}
	if !setup.HasSecret {
		setup.Secret = keyPlaceholder
	}
	setup.Command = fmt.Sprintf("claude mcp add --transport http whatsapp %s --header \"Authorization: Bearer %s\"", endpoint, setup.Secret)
	setup.CommandEnv = fmt.Sprintf("export WHATSAPP_MCP_KEY=%s\nclaude mcp add --transport http whatsapp %s --header \"Authorization: Bearer \\${WHATSAPP_MCP_KEY}\"",
		setup.Secret, endpoint)
	setup.JSON = clientConfig(endpoint, setup.Secret)
	setup.AgentPrompt = agentPrompt(endpoint, setup.Secret, setup.JSON)
	return setup
}

// agentPrompt is written to the assistant, not to the operator: it hands over
// every fact the connection needs and asks the assistant to do the work. That
// is the only instruction that stays correct for clients this panel has never
// heard of.
func agentPrompt(endpoint, secret, config string) string {
	return fmt.Sprintf(`Quero conectar um servidor MCP (Model Context Protocol) em você, para que você possa ler e usar o meu WhatsApp. Configure isso para mim. Se você não puder se configurar sozinho, me explique o passo a passo, bem devagar, para eu fazer na mão.

Dados da conexão:
- Nome do servidor: whatsapp
- Transporte: HTTP (streamable HTTP)
- URL: %s
- Autenticação: cabeçalho HTTP "Authorization: Bearer %s"

A maioria dos clientes MCP aceita esta configuração:
%s

Quando terminar, liste as ferramentas do servidor "whatsapp" e me diga quantas são e qual é o número de telefone conectado.`, endpoint, secret, config)
}

// verificationPrompt is what the operator pastes into the client to confirm the
// connection end to end.
const verificationPrompt = `Use as ferramentas do WhatsApp e me diga:
- se o meu WhatsApp está conectado e qual é o número;
- quantas mensagens você consegue ver e desde quando;
- os nomes das 5 conversas mais recentes.

Não envie mensagem para ninguém. Se alguma coisa falhar, me mostre o erro exato.`

// createKey issues a credential for the selected instance and renders it once.
//
// The secret is shown in the body of this response and never travels through a
// redirect: a query string would land in browser history, proxy logs and the
// referrer of the next request.
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
	// The operator answers one question — where is this going to be used — and
	// that answer is both the connection's name and the instructions they land
	// on. Asking them to invent a label first is the step that used to make a
	// simple thing feel like credential management.
	preferred := clientChoice(r.FormValue("cliente"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = clientLabel(preferred)
	}
	if len(name) > 60 {
		a.fail(w, r, "/", "O apelido da conexão precisa ter até 60 caracteres.")
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
	page := keyPage{
		layout: a.newLayout(r, "Conexão criada", "conectar"),
		Name:   name,
		Secret: secret,
		Setup:  newClientSetupFor(a.endpoint(), secret, preferred),
		Prompt: verificationPrompt,
	}
	a.render(w, "chave", page)
}

// clientConfig renders the MCP client block. It is built here rather than in
// the template so the quoting is Go's problem and not the reader's.
func clientConfig(endpoint, secret string) string {
	config := map[string]any{
		"mcpServers": map[string]any{
			"whatsapp": map[string]any{
				"type":    "http",
				"url":     endpoint,
				"headers": map[string]any{"Authorization": "Bearer " + secret},
			},
		},
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
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

// progress reports the checklist state as data, so the connect page can notice
// a client authenticating without the operator reloading it by hand.
//
// It carries no credential and no instance detail: only whether a key exists
// and whether one has been used.
func (a *webApp) progress(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	keys, err := a.store.ListAPIKeys(r.Context())
	if err != nil {
		http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	connected := false
	for _, key := range keys {
		if !key.LastUsedAt.IsZero() {
			connected = true
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"has_key": len(keys) > 0, "client_connected": connected})
}
