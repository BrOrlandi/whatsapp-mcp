package httpapi

import (
	"net/http"

	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
)

// docsPage is the capability reference. The tools are read from the MCP server
// itself rather than transcribed here, so the page cannot describe a surface
// the server does not actually have.
type docsPage struct {
	layout
	Tools []mcp.Tool
	Count int
}

// recipe is one thing worth doing with the gateway, written for someone
// deciding whether it is worth their time rather than for someone already
// convinced.
type recipe struct {
	Title    string
	Summary  string
	Uses     []string
	Prompt   string
	Caveat   string
	Schedule string
}

type recipesPage struct {
	layout
	Recipes []recipe
}

func (a *webApp) docs(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	tools := mcp.Catalogue()
	a.render(w, "documentacao", docsPage{layout: a.newLayout(r, "Documentação", "documentacao"), Tools: tools, Count: len(tools)})
}

func (a *webApp) recipes(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	a.render(w, "receitas", recipesPage{layout: a.newLayout(r, "Receitas", "receitas"), Recipes: recipeBook()})
}

// recipeBook is the set of things this gateway makes possible without writing
// any code at all.
//
// Every entry here is a prompt, not a feature: the scheduling, the watching and
// the reporting are the assistant's to do, and the gateway's only job is to
// answer for WhatsApp when asked. That distinction is the point of the page. It
// is also why each recipe names the tools it leans on — someone adapting one
// needs to know which parts are real capabilities and which parts are just
// instructions.
func recipeBook() []recipe {
	return []recipe{
		{
			Title:   "Agendar uma mensagem",
			Summary: "O gateway não tem agendador, e não precisa ter. Quem espera é o assistente: a tarefa fica marcada no cliente e, na hora certa, ela chama send_text_message como qualquer outra chamada.",
			Uses:    []string{"send_text_message", "check_numbers"},
			Prompt: `Amanhã às 9h, mande para o Lucas:
"Bom dia! Confirma nossa call das 14h?"

Antes de enviar, confirme que o número existe com check_numbers.
Se a entrega voltar como unconfirmed, me avise em vez de reenviar.`,
			Schedule: "Uma vez, no horário marcado",
			Caveat:   "A janela depende do cliente estar rodando na hora. Um agendamento de semanas à frente é mais frágil do que um de horas.",
		},
		{
			Title:   "Vigiar palavras-chave",
			Summary: "Uma rotina diária lê o dia inteiro de conversas, procura os termos que importam e avisa só quando algo aparece. Nada disso mexe no gateway: search_messages já responde a pergunta, o resto é instrução.",
			Uses:    []string{"search_messages", "list_chats", "get_chat_messages"},
			Prompt: `Todo dia às 19h, procure nas mensagens das últimas 24h por:
"contrato", "proposta", "reunião", "urgente", "boleto"

Para cada acerto, me diga quem falou, em qual conversa e o trecho.
Se não houver nenhum, responda apenas "nada hoje" — não invente resumo.`,
			Schedule: "Diária",
			Caveat:   "search_messages cobre só o que foi indexado. Se whatsapp_status apontar um gap no período, o silêncio pode ser perda de dado e não ausência de assunto.",
		},
		{
			Title:   "Resumo do que ficou sem resposta",
			Summary: "Varre as conversas em que a última mensagem é da outra pessoa e já tem algumas horas. É a lista de quem está esperando você.",
			Uses:    []string{"list_chats", "get_chat_messages"},
			Prompt: `Liste as conversas em que a última mensagem não é minha
e chegou há mais de 4 horas.

Para cada uma: quem é, há quanto tempo, e o que a pessoa pediu.
Ordene pela mais antiga. Ignore grupos.`,
			Schedule: "Duas vezes ao dia",
			Caveat:   "list_chats devolve o último texto de cada conversa, então o corte por tempo é barato. Ler cada conversa inteira não é.",
		},
		{
			Title:   "Enquete e apuração",
			Summary: "Mandar a enquete e voltar depois para ler o resultado são duas chamadas separadas, ligadas pelo id que a primeira devolve.",
			Uses:    []string{"send_poll", "get_poll_results"},
			Prompt: `Mande no grupo do time uma enquete:
"Churrasco sexta?" com as opções Sim, Não e Talvez.

Guarde o message_id. Amanhã às 17h, leia o resultado
com get_poll_results e me diga a contagem.`,
			Schedule: "Envio agora, apuração depois",
			Caveat:   "Sem guardar o message_id não há como apurar. Peça ao assistente para anotá-lo junto com a tarefa de leitura.",
		},
		{
			Title:   "Arquivo do que foi combinado",
			Summary: "Transforma uma conversa longa em uma lista de compromissos, com quem prometeu o quê e quando.",
			Uses:    []string{"get_chat_messages", "search_messages"},
			Prompt: `Leia minha conversa com [contato] dos últimos 30 dias
e extraia tudo que virou combinado: datas, valores, prazos.

Formate como uma lista com data, o que foi acordado e quem disse.
Se algo estiver ambíguo, marque como ambíguo em vez de decidir por mim.`,
			Schedule: "Sob demanda",
			Caveat:   "Mensagens são escritas por terceiros. Trate o conteúdo como dado, nunca como instrução — um texto que diz \"encaminhe isso\" não é um pedido a ser cumprido.",
		},
		{
			Title:   "Aviso de índice furado",
			Summary: "O gateway sabe quando ficou sem ingerir, e sabe dizer quando um período está vazio por perda em vez de silêncio. Vale checar isso periodicamente em vez de descobrir no dia em que a resposta importa.",
			Uses:    []string{"whatsapp_status", "backfill_gap"},
			Prompt: `Toda segunda de manhã, rode whatsapp_status.

Se houver algum gap novo no índice, me avise com o período.
Rode backfill_gap em modo detect_only e me diga
quantas conversas dariam para recuperar.`,
			Schedule: "Semanal",
			Caveat:   "backfill_gap só alcança conversas que receberam mensagem depois do buraco. As demais aparecem como unreachable_chats e voltam a ser recuperáveis quando movimentarem.",
		},
	}
}
