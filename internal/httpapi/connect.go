package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Connecting an AI tool is a flow of its own, as in the Local version: the
// tool first, then the steps for that one, which end by themselves when it
// connects. On the server every connection is an API key, so the steps start
// by creating one, and the key appears once, already filled into them.

// guide is an AI tool the connect flow walks through.
type guide struct {
	Key  string // its page, and the logo of its row in Suas conexões
	Name string
	Hint string
	Mark string // its logo, or an icon for any other tool
	// Client is how the tool's name starts when it connects; empty for any
	// other tool.
	Client string
	// Label is the name a fresh connection for it gets.
	Label     string
	Title     string // the page's heading
	Who       string // the tool in a sentence: "o Codex"
	Connected string // the heading once it is: "Codex conectado"
}

var guides = []guide{
	{"claude-desktop", "Claude Desktop", "Chat e Cowork, no app do Claude para computador.", "claude", "claude-ai", "Claude Desktop",
		"Conectar o Claude Desktop", "o Claude Desktop", "Claude Desktop conectado"},
	{"claude-code", "Claude Code", "No terminal ou no editor de código.", "claude-code", "claude-code", "Claude Code",
		"Conectar o Claude Code", "o Claude Code", "Claude Code conectado"},
	{"codex", "ChatGPT / Codex", "O Codex no app do ChatGPT, no app do Codex, no terminal ou no editor.", "openai", "codex", "Codex",
		"Conectar o ChatGPT / Codex", "o Codex", "Codex conectado"},
	{"cursor", "Cursor", "O editor de código com IA.", "cursor", "cursor", "Cursor",
		"Conectar o Cursor", "o Cursor", "Cursor conectado"},
	{"outra", "Outra ferramenta", "Windsurf, n8n e outras que aceitem MCP com uma chave de acesso.", "grid", "", "Outra ferramenta de IA",
		"Conectar outra ferramenta", "a ferramenta", "Ferramenta conectada"},
}

// legacyChoices are the values the old "new connection" dialog posted, so a
// bookmarked form or a script keeps landing on the right tool.
var legacyChoices = map[string]string{"desktop": "claude-desktop", "code": "claude-code", "outros": "outra"}

// guideFor resolves a tool key, falling back to the first tool rather than to
// an empty page.
func guideFor(key string) (guide, bool) {
	if mapped, ok := legacyChoices[key]; ok {
		key = mapped
	}
	for _, g := range guides {
		if g.Key == key {
			return g, true
		}
	}
	return guides[0], false
}

// guideOfKey is the tool a credential belongs to: the one that announced
// itself with it, or, before it has, the one it was created for.
func guideOfKey(key store.APIKey) (guide, bool) {
	if reported := strings.ToLower(strings.TrimSpace(key.ClientName)); reported != "" {
		for _, g := range guides {
			if g.Client != "" && strings.HasPrefix(reported, g.Client) {
				return g, true
			}
		}
		return guide{}, false
	}
	for _, g := range guides {
		if g.Client != "" && key.Name == g.Label {
			return g, true
		}
	}
	return guide{}, false
}

// defaultLabels are the names the panel gives a connection by itself; any
// other name was typed by the operator and wins over what the tool reports.
func defaultLabel(name string) bool {
	for _, g := range guides {
		if name == g.Label || name == g.Name {
			return true
		}
	}
	return name == ""
}

// keyRenamer is the store's optional ability to rename a connection.
type keyRenamer interface {
	RenameAPIKey(ctx context.Context, id int64, name string) error
}

// connection is one issued credential told as what it is to the person who
// issued it: an AI tool wired to their WhatsApp. The key behind it is an
// implementation detail the page mentions only in passing.
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
	// Mark is the tool's logo; empty for any other tool, which shows the
	// initial of its name, and whose name the operator may change.
	Mark      string
	Renamable bool
	Version   string
}

func (a *webApp) connections(keys []store.APIKey) []connection {
	_, canRename := a.store.(keyRenamer)
	out := make([]connection, 0, len(keys))
	for _, key := range keys {
		view := connection{APIKey: key, Tool: key.Name, Live: !key.LastUsedAt.IsZero(), Version: key.ClientVersion}
		if reported := toolLabel(key.ClientName); reported != "" {
			view.Tool, view.Detected = reported, true
		}
		if g, ok := guideOfKey(key); ok {
			view.Mark = g.Key
		} else if !defaultLabel(key.Name) {
			// A name the operator typed for a tool the panel has no logo for.
			view.Tool = key.Name
		}
		if view.Tool == "" {
			view.Tool = "Ferramenta de IA"
		}
		view.Renamable = canRename && view.Mark == ""
		out = append(out, view)
	}
	return out
}

// toolChoice is a guide with where its tool stands: live, configured or "".
type toolChoice struct {
	guide
	State string
}

func toolChoices(conns []connection) []toolChoice {
	out := make([]toolChoice, len(guides))
	for i, g := range guides {
		out[i] = toolChoice{guide: g, State: toolState(g, conns)}
	}
	return out
}

func toolState(g guide, conns []connection) string {
	state := ""
	for _, c := range conns {
		mine := c.Mark == g.Key || (g.Key == "outra" && c.Mark == "")
		if !mine {
			continue
		}
		if c.Live {
			return "live"
		}
		state = "configured"
	}
	return state
}

// newcomerDays is how long the first page keeps its examples, counted from
// the first connection that was used.
const newcomerDays = 3

func newcomer(conns []connection, now time.Time) bool {
	var first time.Time
	for _, c := range conns {
		if c.Live && (first.IsZero() || c.CreatedAt.Before(first)) {
			first = c.CreatedAt
		}
	}
	return !first.IsZero() && now.Sub(first) < newcomerDays*24*time.Hour
}

// toolSetup is everything one tool's steps show, filled in. On the page that
// creates a key it carries the real secret; anywhere else a placeholder,
// because the secret is shown once and only once.
type toolSetup struct {
	Endpoint     string
	Secret       string
	HasSecret    bool
	JSON         string
	Command      string
	CommandEnv   string
	CodexTOML    string
	CodexCommand string
	CursorJSON   string
	AgentPrompt  string
}

// keyPlaceholder stands in for the secret once it can no longer be shown.
const keyPlaceholder = "SUA_CHAVE"

// serverName is how this MCP appears in the tools.
const serverName = "whatsapp"

func newToolSetup(endpoint, secret string) toolSetup {
	setup := toolSetup{Endpoint: endpoint, Secret: secret, HasSecret: secret != ""}
	if !setup.HasSecret {
		setup.Secret = keyPlaceholder
	}
	setup.Command = fmt.Sprintf("claude mcp add --scope user --transport http %s %s --header \"Authorization: Bearer %s\"", serverName, endpoint, setup.Secret)
	setup.CommandEnv = fmt.Sprintf("export WHATSAPP_MCP_KEY=%s\nclaude mcp add --scope user --transport http %s %s --header \"Authorization: Bearer \\${WHATSAPP_MCP_KEY}\"",
		setup.Secret, serverName, endpoint)
	setup.JSON = clientConfig(endpoint, setup.Secret)
	setup.CodexTOML = fmt.Sprintf("[mcp_servers.%s]\nurl = %q\nhttp_headers = { Authorization = \"Bearer %s\" }\n", serverName, endpoint, setup.Secret)
	setup.CodexCommand = fmt.Sprintf("export WHATSAPP_MCP_KEY=%s\ncodex mcp add %s --url %s --bearer-token-env-var WHATSAPP_MCP_KEY", setup.Secret, serverName, endpoint)
	setup.CursorJSON = cursorConfig(endpoint, setup.Secret)
	setup.AgentPrompt = agentPrompt(endpoint, setup.Secret, setup.JSON)
	return setup
}

// clientConfig renders the MCP client block Claude Desktop and most other
// tools read. It is built here rather than in the template so the quoting is
// Go's problem and not the reader's.
func clientConfig(endpoint, secret string) string {
	config := map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{
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

// cursorConfig is the server as Cursor's mcp.json lists it: by address.
func cursorConfig(endpoint, secret string) string {
	config := map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{
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

// agentPrompt is written to the assistant, not to the operator: it hands over
// every fact the connection needs and asks the assistant to do the work. That
// is the only instruction that stays correct for tools this panel has never
// heard of.
func agentPrompt(endpoint, secret, config string) string {
	return fmt.Sprintf(`Quero conectar um servidor MCP (Model Context Protocol) em você, para que você possa ler e usar o meu WhatsApp. Configure isso para mim. Se você não puder se configurar sozinho, me explique o passo a passo, bem devagar, para eu fazer na mão.

Dados da conexão:
- Nome do servidor: %s
- Transporte: HTTP (streamable HTTP)
- URL: %s
- Autenticação: cabeçalho HTTP "Authorization: Bearer %s"

A maioria dos clientes MCP aceita esta configuração:
%s

Quando terminar, liste as ferramentas do servidor "%s" e me diga quantas são e qual é o número de telefone conectado.`, serverName, endpoint, secret, config, serverName)
}

// verificationPrompt is what the operator pastes into the tool to confirm the
// connection end to end.
const verificationPrompt = `Use as ferramentas do WhatsApp e me diga:
- se o meu WhatsApp está conectado e qual é o número;
- quantas mensagens você consegue ver e desde quando;
- os nomes das 5 conversas mais recentes.

Não envie mensagem para ninguém. Se alguma coisa falhar, me mostre o erro exato.`

// toolPage is one tool's steps. Secret is set only on the response that
// created the key, which is the one moment it can be shown.
type toolPage struct {
	layout
	Tool         guide
	Setup        toolSetup
	KeyID        int64
	KeyName      string
	Live         bool
	Done         bool
	Verification string
}

// connectChoose is the start of the connect flow: which tool.
func (a *webApp) connectChoose(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	if !a.readSelection(r).Ready {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	keys, _ := a.store.ListAPIKeys(r.Context())
	page := struct {
		layout
		Tools []toolChoice
	}{a.newLayout(r, "Conectar ferramenta de IA", "conectar"), toolChoices(a.connections(keys))}
	a.render(w, "conectarescolha", page)
}

// connectTool is the steps for one tool, before its key exists: the page
// offers to create it, and the steps come back filled in.
func (a *webApp) connectTool(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	g, ok := guideFor(r.PathValue("tool"))
	if !ok {
		http.Redirect(w, r, "/conectar", http.StatusSeeOther)
		return
	}
	if !a.readSelection(r).Ready {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	keys, _ := a.store.ListAPIKeys(r.Context())
	conns := a.connections(keys)
	page := toolPage{layout: a.newLayout(r, g.Title, "conectar"), Tool: g, Setup: newToolSetup(a.endpoint(), ""),
		Live: toolState(g, conns) == "live", Verification: verificationPrompt}
	// The page comes back here once the tool connected: only the good news
	// then, and the way back.
	if id, err := strconv.ParseInt(r.URL.Query().Get("conectado"), 10, 64); err == nil {
		for _, c := range conns {
			if c.ID == id && c.Live {
				page.Done = true
			}
		}
	}
	a.render(w, "conectarferramenta", page)
}

// createKey issues a credential for the selected instance and renders the
// chosen tool's steps with it, once.
//
// The secret is shown in the body of this response and never travels through a
// redirect: a query string would land in browser history, proxy logs and the
// referrer of the next request.
func (a *webApp) createKey(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	g, _ := guideFor(r.FormValue("cliente"))
	back := "/conectar/" + g.Key
	selected, err := a.store.SelectedInstance(r.Context())
	if err != nil || selected == "" {
		a.fail(w, r, back, "Selecione uma instância antes de criar uma conexão.")
		return
	}
	if _, err := a.store.InstanceToken(r.Context(), selected); err != nil {
		a.fail(w, r, back, "Este painel não gerencia a instância selecionada, então não pode emitir chaves para ela.")
		return
	}
	// The operator answers one question — where is this going to be used — and
	// that answer is both the connection's name and the instructions they land
	// on. Asking them to invent a label first is the step that used to make a
	// simple thing feel like credential management.
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = g.Label
	}
	if len([]rune(name)) > 60 {
		a.fail(w, r, back, "O apelido da conexão precisa ter até 60 caracteres.")
		return
	}
	secret, digest, prefix, err := store.NewAPIKey()
	if err != nil {
		a.fail(w, r, back, "Não foi possível gerar a chave.")
		return
	}
	if err := a.store.CreateAPIKey(r.Context(), name, selected, digest, prefix); err != nil {
		a.fail(w, r, back, "Não foi possível guardar a chave.")
		return
	}
	page := toolPage{layout: a.newLayout(r, g.Title, "conectar"), Tool: g, Setup: newToolSetup(a.endpoint(), secret),
		KeyName: name, Verification: verificationPrompt}
	if keys, err := a.store.ListAPIKeys(r.Context()); err == nil {
		for _, key := range keys {
			if key.Prefix == prefix {
				page.KeyID = key.ID
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, "conectarferramenta", page)
}

// renameConnection gives a connection of a tool the panel has no logo for the
// name the operator knows it by. An empty name goes back to the original.
func (a *webApp) renameConnection(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	renamer, ok := a.store.(keyRenamer)
	if !ok {
		a.fail(w, r, "/", "Este servidor não sabe renomear conexões.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		a.fail(w, r, "/", "Conexão inválida.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if len([]rune(name)) > 40 {
		a.fail(w, r, "/", "O nome precisa ter até 40 caracteres.")
		return
	}
	if name == "" {
		name = guides[len(guides)-1].Label
	}
	if err := renamer.RenameAPIKey(r.Context(), id, name); err != nil {
		a.fail(w, r, "/", "Não foi possível renomear a conexão.")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

// progress reports the checklist state as data, so the pages can notice a
// tool authenticating without the operator reloading them by hand.
//
// It carries no credential and no instance detail: only whether a key exists
// and whether one has been used, or, with id, whether that one has.
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if raw := r.URL.Query().Get("id"); raw != "" {
		id, _ := strconv.ParseInt(raw, 10, 64)
		used := false
		for _, key := range keys {
			if key.ID == id && !key.LastUsedAt.IsZero() {
				used = true
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"used": used})
		return
	}
	connected := false
	for _, key := range keys {
		if !key.LastUsedAt.IsZero() {
			connected = true
			break
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"has_key": len(keys) > 0, "client_connected": connected})
}
