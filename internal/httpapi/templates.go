package httpapi

// pages is the template source the panel parses.
var pages = pageSource

// pageSource holds every template the control panel renders.
//
// The markup is the Local version's — the masthead with the gear, the tabs
// with icons, the cards, the connect flow, Ajuda and Configurações — so the
// server panel and the desktop app look like one product. What differs is what
// a server has and an app does not: a sign-in, a licence, several WhatsApp
// accounts and an API key per connection. Dialogs use the :target selector so
// creating something never needs JavaScript to work.
const pageSource = `
{{define "head"}}<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="/favicon.svg" type="image/svg+xml"><link rel="alternate icon" href="/favicon.ico" sizes="16x16 32x32 48x48"><link rel="apple-touch-icon" href="/apple-touch-icon.png"><script src="/assets/theme.js"></script>{{if .Refresh}}<meta http-equiv="refresh" content="5">{{end}}<title>{{.Title}} · WhatsApp MCP</title><style>{{css}}</style></head><body><div class="shell">{{end}}

{{define "foot"}}
<footer class="colophon">
<p class="colophon__line">
<a class="colophon__link" href="{{authorURL}}" rel="noopener noreferrer" target="_blank">{{author}}</a>
<span class="colophon__sep">&middot;</span>
<a class="colophon__link" href="{{repositoryURL}}" rel="noopener noreferrer" target="_blank">{{template "githubmark"}}<span>C&oacute;digo-fonte</span></a>
<span class="colophon__sep">&middot;</span>
<span class="colophon__version" title="Vers&atilde;o em execu&ccedil;&atilde;o">v{{version}}</span>
{{with supportURL}}<span class="colophon__sep">&middot;</span>
<a class="colophon__support" href="{{.}}" rel="noopener noreferrer" target="_blank">Apoie o projeto</a>{{end}}
</p>
</footer>
<script src="/assets/app.js" defer></script><script src="/assets/password.js" defer></script></div></body></html>{{end}}

{{define "githubmark"}}<svg class="colophon__icon" viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false"><path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.4 7.4 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"/></svg>{{end}}

{{/* The way into Configurações, in the top right corner of every page. */}}
{{define "settingslink"}}<a class="gear" href="/configuracoes" aria-label="Configurações" title="Configurações"{{if eq .Active "configuracoes"}} aria-current="page"{{end}}>{{icon "gear"}}</a>{{end}}

{{define "brandmark"}}<span class="brand__mark">{{logo}}</span><span class="brand__name">WhatsApp MCP</span>{{end}}

{{define "nav"}}
<header class="masthead"><a class="brand" href="/">{{template "brandmark"}}</a>
<div class="masthead__tools">{{template "settingslink" .}}</div></header>
<nav class="nav" aria-label="Seções">
<a href="/"{{if eq .Active "conectar"}} aria-current="page"{{end}}>{{icon "plug"}}<span>Conectar MCP</span></a>
<a href="/whatsapp"{{if eq .Active "whatsapp"}} aria-current="page"{{end}}>{{icon "chat"}}<span>WhatsApp</span></a>
<a href="/status"{{if eq .Active "status"}} aria-current="page"{{end}}>{{icon "activity"}}{{if ne .SessionTone "ok"}}<span class="sr-only">Atenção: </span>{{end}}<span class="nav__label">Status</span>{{if ne .SessionTone "ok"}}<span class="nav__alert{{if eq .SessionTone "warn"}} nav__alert--warn{{end}}" aria-hidden="true">!</span>{{end}}</a>
<a href="/funcoes"{{if eq .Active "funcoes"}} aria-current="page"{{end}}>{{icon "list"}}<span>Funções</span></a>
<a href="/receitas"{{if eq .Active "receitas"}} aria-current="page"{{end}}>{{icon "book"}}<span>Receitas</span></a>
<a href="/ajuda"{{if eq .Active "ajuda"}} aria-current="page"{{end}}>{{icon "help"}}<span>Ajuda</span></a>
</nav>
{{template "updatebanner" .}}
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{end}}

{{/* The banner lives in "nav" rather than in the footer on purpose: nav is
rendered only on the pages behind the session cookie, and "this instance is
running an outdated version" is not something to tell whoever loads the sign-in
page. */}}

{{/* Connecting an AI tool is a flow of its own: the tool first, then the
steps for that one, which end when it connects. No tabs on the way. */}}
{{define "flowhead"}}
<header class="masthead"><a class="brand" href="/">{{template "brandmark"}}</a>
<div class="masthead__tools">{{template "settingslink" .}}</div></header>
<div class="flow">
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{end}}

{{define "toolpicks"}}<ul class="toolpicks">
{{range .}}<li><a class="toolpick" href="/conectar/{{.Key}}"><span class="toolpick__icon">{{toolmark .Mark}}</span>
<span class="toolpick__text"><span class="toolpick__name">{{.Name}}</span><span class="toolpick__hint">{{.Hint}}</span></span>
{{if eq .State "live"}}<span class="pill pill--ok">Conectado</span>{{else if eq .State "configured"}}<span class="pill pill--warn">Aguardando</span>{{end}}
<span class="toolpick__go" aria-hidden="true">{{icon "chevron"}}</span></a></li>
{{end}}</ul>{{end}}

{{define "conectarescolha"}}{{template "head" .}}{{template "flowhead" .}}
<a class="flow__back" href="/">{{icon "back"}}Voltar</a>
<h1>Conectar ferramenta de IA ao MCP</h1>
<p class="lead">Onde você vai usar o seu WhatsApp?</p>
{{template "toolpicks" .Tools}}
<p class="muted flow__note">Cada ferramenta ganha uma chave de acesso própria, e funciona em qualquer computador: o servidor fica na internet. O Claude no navegador ou no celular e o ChatGPT na web ainda não aceitam essa chave.</p>
</div>
{{template "foot"}}{{end}}

{{define "conectarferramenta"}}{{template "head" .}}{{template "flowhead" .}}
{{if not .Done}}<a class="flow__back" href="/conectar">{{icon "back"}}Escolher outra ferramenta</a>{{end}}
<div class="flow__title"><span class="toolpick__icon">{{toolmark .Tool.Mark}}</span><h1>{{.Tool.Title}}</h1></div>

{{if .Done}}
<section class="card card--accent">
<div class="card__head"><h2>{{.Tool.Connected}}</h2><span class="pill pill--ok">Conectado</span></div>
<div class="card__body stack">
<p class="muted">Já pode pedir coisas do seu WhatsApp para a sua IA. Para testar, mande isto no chat:</p>
<div class="snippet"><pre class="plain" data-copy><code>{{.Verification}}</code></pre></div>
<div class="actions"><a class="btn" href="/">Voltar ao painel</a></div>
</div></section>
{{else if not .Setup.HasSecret}}
{{if .Live}}<p class="alert alert--ok">{{.Tool.Connected}}. Crie outra conexão só se quiser configurar de novo, ou em outro computador.</p>{{end}}
<section class="card">
<div class="card__body">
<ol class="flowsteps">
<li class="flowstep"><h2 class="flowstep__title">Crie a chave de acesso desta conexão</h2>
<p class="muted">Cada ferramenta usa uma chave própria, que dá acesso ao seu WhatsApp neste servidor. Ela aparece uma única vez, na tela seguinte, já preenchida no passo a passo.</p>
<form method="post" action="/chaves" data-busy="Criando…"><input type="hidden" name="cliente" value="{{.Tool.Key}}">
{{if eq .Tool.Key "outra"}}<label class="field" for="key-name"><span class="field__label">Nome da ferramenta</span>
<span class="field__hint">Opcional, só para você reconhecer depois. Ex.: &ldquo;Windsurf&rdquo;, &ldquo;n8n&rdquo;.</span></label>
<input id="key-name" type="text" name="name" maxlength="60" placeholder="Outra ferramenta de IA" autocomplete="off" spellcheck="false">{{end}}
<div class="actions"><button class="btn" type="submit">Criar a chave e ver o passo a passo</button></div>
</form></li>
<li class="flowstep"><h2 class="flowstep__title">Configure {{.Tool.Who}}</h2>
<p class="muted">Com a chave criada, os passos aparecem aqui, prontos para copiar e colar.</p></li>
<li class="flowstep"><h2 class="flowstep__title">Espere a conexão</h2>
<p class="muted">Esta tela avisa sozinha quando {{.Tool.Who}} se conectar.</p></li>
</ol>
</div></section>
{{else}}
<div class="secret">
<p class="secret__title">Guarde esta chave agora</p>
<code class="secret__value">{{.Setup.Secret}}</code>
<p class="muted">Ela aparece uma única vez, aqui, e já vem preenchida nos passos abaixo, então normalmente você nem precisa copiá-la à parte. Se quiser guardar, use o seu gerenciador de senhas — nunca um arquivo que você compartilha.</p>
</div>
<section class="card">
<div class="card__body">
<ol class="flowsteps">
{{if eq .Tool.Key "claude-desktop"}}
<li class="flowstep"><h2 class="flowstep__title">Abra a configuração do Claude Desktop</h2>
<p class="muted">No Claude Desktop, vá em <strong>Configurações → Desenvolvedor → Editar configuração</strong>. Ainda não tem o app? <a href="https://claude.ai/download" target="_blank" rel="noopener">Baixar o Claude Desktop</a>.</p></li>
<li class="flowstep"><h2 class="flowstep__title">Cole a configuração do WhatsApp</h2>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">claude_desktop_config.json</span></div>
<pre data-copy><code>{{.Setup.JSON}}</code></pre></div>
<p class="muted">Se já houver outros servidores no arquivo, acrescente só o trecho <code>"whatsapp"</code> dentro de <code>mcpServers</code>, sem apagar o resto.</p></li>
<li class="flowstep"><h2 class="flowstep__title">Feche o Claude Desktop e abra de novo</h2>
<p class="muted">Só assim ele carrega o WhatsApp.</p></li>
{{else if eq .Tool.Key "claude-code"}}
<li class="flowstep"><h2 class="flowstep__title">Adicione o WhatsApp ao Claude Code</h2>
<p class="muted">Rode este comando no terminal, de qualquer pasta.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Comando</span></div>
<pre data-copy><code>{{.Setup.Command}}</code></pre></div>
<details class="resend"><summary>Prefiro não deixar a chave na configuração</summary>
<p class="muted">Nesta forma o Claude Code lê a chave de <code>WHATSAPP_MCP_KEY</code>, que você exporta no seu shell. Confira depois com <code>claude mcp list</code>.</p>
<div class="snippet"><pre data-copy><code>{{.Setup.CommandEnv}}</code></pre></div>
</details></li>
<li class="flowstep"><h2 class="flowstep__title">Abra uma sessão nova do Claude Code</h2>
<p class="muted">Uma sessão que já estava aberta não vê o WhatsApp.</p></li>
{{else if eq .Tool.Key "codex"}}
<li class="flowstep"><h2 class="flowstep__title">Adicione o WhatsApp ao Codex</h2>
<p class="muted">Acrescente este trecho ao arquivo de configuração do Codex, que vale para o app do ChatGPT, o app do Codex, o terminal e o editor de código.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">~/.codex/config.toml</span></div>
<pre data-copy><code>{{.Setup.CodexTOML}}</code></pre></div>
<details class="resend"><summary>Prefiro rodar um comando</summary>
<p class="muted">Neste caso a chave fica numa variável de ambiente, que precisa estar exportada sempre que o Codex abrir.</p>
<div class="snippet"><pre data-copy><code>{{.Setup.CodexCommand}}</code></pre></div>
</details></li>
<li class="flowstep"><h2 class="flowstep__title">Abra uma conversa nova no Codex</h2>
<p class="muted">Pode ser no app do ChatGPT, no app do Codex, no terminal ou no editor. Se o app estiver aberto, feche e abra de novo.</p></li>
{{else if eq .Tool.Key "cursor"}}
<li class="flowstep"><h2 class="flowstep__title">Adicione o WhatsApp ao Cursor</h2>
<p class="muted">Abra o arquivo abaixo, que o Cursor também abre pelas configurações de MCP dele, e cole o texto. Se já houver outros servidores no arquivo, acrescente só o trecho <code>"whatsapp"</code> dentro de <code>mcpServers</code>, sem apagar o resto.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">~/.cursor/mcp.json</span></div>
<pre data-copy><code>{{.Setup.CursorJSON}}</code></pre></div></li>
<li class="flowstep"><h2 class="flowstep__title">Feche o Cursor e abra de novo</h2>
<p class="muted">Depois, peça ao agente do Cursor o que quiser do seu WhatsApp.</p></li>
{{else}}
<li class="flowstep"><h2 class="flowstep__title">Peça para a sua ferramenta se configurar</h2>
<p class="muted">Serve para o Windsurf, o n8n e qualquer outra que aceite MCP. Copie o texto abaixo e mande no chat dela.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Copie e mande para a sua ferramenta</span></div>
<pre class="plain" data-copy><code>{{.Setup.AgentPrompt}}</code></pre></div></li>
<li class="flowstep"><h2 class="flowstep__title">Reinicie a ferramenta</h2>
<p class="muted">Quando ela terminar a configuração, feche e abra de novo, ou comece uma conversa nova.</p></li>
{{end}}
<li class="flowstep"><h2 class="flowstep__title">Espere a conexão</h2><p class="busy" role="status"{{if .KeyID}} data-wait-key="{{.KeyID}}" data-wait-tool="{{.Tool.Key}}"{{end}}><span class="spinner" aria-hidden="true"></span>Esta tela avisa quando {{.Tool.Who}} se conectar.</p></li>
</ol>
</div></section>
{{end}}
</div>
{{template "foot"}}{{end}}

{{define "conectar"}}{{template "head" .}}{{template "nav" .}}
<h1>Seu WhatsApp nas suas ferramentas de IA</h1>
<p class="lead">Aqui você vê se está tudo funcionando e liga o seu WhatsApp a uma ferramenta de inteligência artificial que aceite MCP, como o Claude, o Codex ou a que você usar.</p>

{{if .Ready}}
{{/* The two sentences that answer "está tudo certo?" without anybody having to
     read a status page. The progress marker rides here because this is the
     block that changes the moment a tool connects. */}}
<section class="overview"{{if and .HasKey (not .ClientConnected)}} data-progress data-connected="false"{{end}}>
<div class="overview__item overview__item--ok">
<span class="overview__icon" aria-hidden="true">&#10003;</span>
<div class="overview__body">
<p class="overview__title">WhatsApp conectado</p>
<p class="overview__detail">{{if .Phone}}<strong class="overview__phone">{{.Phone}}</strong>{{end}}{{with .Account}}{{.}}{{else}}Conta &ldquo;{{$.InstanceName}}&rdquo;{{end}}</p>
</div></div>

{{if .ClientConnected}}
<div class="overview__item overview__item--ok">
<span class="overview__icon" aria-hidden="true">&#10003;</span>
<div class="overview__body">
<p class="overview__title">{{plural .LiveCount "ferramenta de IA conectada ao MCP" "ferramentas de IA conectadas ao MCP"}}</p>
<p class="overview__detail">Já pode pedir coisas do seu WhatsApp para a sua IA. Última vez em uso: {{relativeSince .LastUse}}.</p>
</div></div>
{{else if .HasKey}}
<div class="overview__item overview__item--wait">
<span class="overview__icon" aria-hidden="true">&hellip;</span>
<div class="overview__body">
<p class="overview__title">Esperando a sua ferramenta de IA</p>
<p class="overview__detail">A conexão já existe. Falta colar a configuração na ferramenta e reiniciar ela. Esta tela avisa sozinha quando ela aparecer.</p>
</div></div>
{{else}}
<div class="overview__item">
<span class="overview__icon" aria-hidden="true">+</span>
<div class="overview__body">
<p class="overview__title">Nenhuma ferramenta de IA conectada</p>
<p class="overview__detail">Falta um passo: ligar o seu assistente de IA a este WhatsApp.</p>
</div></div>
{{end}}
</section>

<div class="hero"><a class="btn btn--big" href="/conectar">Conectar ferramenta de IA ao MCP</a></div>

<section class="card">
<div class="card__head"><h2>Suas conexões</h2>{{if .Connections}}<a class="btn btn--ghost btn--small" href="/conectar">Nova conexão</a>{{end}}</div>
<div class="card__body">
{{if .Connections}}
<ul class="rows">
{{range $i, $c := .Connections}}<li class="row{{if not .Live}} row--waiting{{end}}">
{{if .Mark}}<span class="tool-mark tool-mark--logo" aria-hidden="true">{{toolmark .Mark}}</span>{{else}}<span class="tool-mark" aria-hidden="true">{{initial .Tool}}</span>{{end}}
<span class="row__main"><span class="row__title">{{.Tool}}</span>
<span class="row__meta">{{if .Live}}Funcionando &middot; usada {{relativeSince .LastUsedAt}}{{else}}Ainda não se conectou &middot; configure a ferramenta e reinicie{{end}} &middot; <span class="mono">{{.Prefix}}&hellip;</span>{{with .Version}} <span class="tip tip--quiet" tabindex="0" aria-describedby="tip-versao-{{$i}}"><span aria-hidden="true">?</span><span class="tip__body" role="tooltip" id="tip-versao-{{$i}}">{{$c.Tool}} {{.}}</span></span>{{end}}</span></span>
{{if .Live}}<span class="pill pill--ok">Conectada</span>{{else}}<span class="pill pill--warn">Aguardando</span>{{end}}
{{if .Renamable}}<a class="btn btn--quiet btn--small" href="#renomear-{{$i}}">Renomear</a>{{end}}
<a class="btn btn--danger btn--small" href="#desconectar-{{$i}}">Desconectar</a>
</li>{{end}}
</ul>
<p class="muted">Cada ferramenta tem a sua própria chave de acesso. Desconectar uma vale na hora e não mexe nas outras.</p>
{{else}}
<div class="empty"><p class="empty__title">Nenhuma ferramenta de IA conectada ainda</p>
<p class="muted">Clique no botão acima, escolha onde você vai usar e siga o passo a passo. Leva menos de um minuto.</p></div>
{{end}}
</div></section>

{{/* The first days only: after that the examples live in Ajuda. */}}
{{if and .ClientConnected .Newcomer}}
<section class="card">
<div class="card__head"><h2>Experimente pedir</h2></div>
<div class="card__body">
<p class="muted">Escreva isso no chat da sua ferramenta de IA. Ler e procurar é seguro: mandar mensagem só acontece quando você pede.</p>
<ul class="prompts">
{{range .Prompts}}<li class="prompt"><div class="snippet"><pre data-copy><code>{{.}}</code></pre></div></li>{{end}}
</ul>
<p class="muted" style="margin-bottom:0">Mais exemplos e respostas para dúvidas na <a href="/ajuda">Ajuda</a>.</p>
</div></section>
{{end}}

{{else}}
<section class="card">
<div class="card__head"><h2>Conecte o seu WhatsApp primeiro</h2>{{with .SessionLabel}}<span class="pill pill--{{$.SessionTone}}">{{.}}</span>{{end}}</div>
<div class="card__body">
<div class="empty"><p class="empty__title">Ainda não dá para conectar uma ferramenta de IA</p><p class="muted">{{.Notice}}</p>
<div class="actions" style="justify-content:center;margin-top:14px">
{{if .NeedsActivation}}<a class="btn" href="/instalacao">Ativar a licença</a>{{end}}
{{if .NeedsPairing}}<a class="btn" href="/pair">Ler o QR code</a>{{end}}
<a class="btn btn--ghost" href="/whatsapp">Ir para WhatsApp</a>
</div></div>
</div></section>
{{end}}

{{range $i, $c := .Connections}}
{{if .Renamable}}<div class="overlay" id="renomear-{{$i}}" role="dialog" aria-modal="true" aria-labelledby="renomear-{{$i}}-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="renomear-{{$i}}-titulo">Renomear esta conexão</h2><a class="dialog__close" href="#" aria-label="Fechar">&times;</a></div>
<div class="dialog__body">
<form method="post" action="/conexoes/renomear"><input type="hidden" name="id" value="{{.ID}}">
<label class="field" for="nome-{{$i}}"><span class="field__label">Nome</span><span class="field__hint">Como esta ferramenta aparece aqui. Deixe em branco para voltar ao nome original.</span></label>
<input id="nome-{{$i}}" type="text" name="name" value="{{.Tool}}" maxlength="40" autocomplete="off" spellcheck="false">
<div class="actions actions--end" style="margin-top:14px"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn" type="submit">Salvar</button></div>
</form>
</div></div></div>{{end}}
<div class="overlay" id="desconectar-{{$i}}" role="dialog" aria-modal="true" aria-labelledby="desconectar-{{$i}}-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="desconectar-{{$i}}-titulo">Desconectar esta ferramenta?</h2><a class="dialog__close" href="#" aria-label="Fechar">&times;</a></div>
<div class="dialog__body">
<div class="target"><span class="target__name">{{.Tool}}</span><span class="target__meta mono">{{.Prefix}}&hellip;</span></div>
<p><strong>{{.Tool}}</strong> perde o acesso ao seu WhatsApp na mesma hora. As suas outras conexões continuam funcionando normalmente.</p>
<p class="muted">Se quiser ligar de novo depois, é só criar uma conexão nova.</p>
<form method="post" action="/chaves/revogar"><input type="hidden" name="id" value="{{.ID}}">
<div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn btn--danger" type="submit">Desconectar</button></div>
</form>
</div></div></div>
{{end}}
{{template "foot"}}{{end}}

{{define "instancias"}}{{template "head" .}}{{template "nav" .}}
<h1>WhatsApp</h1>
<p class="lead">As contas de WhatsApp conectadas a este servidor e o que já chegou delas. O MCP usa uma por vez.</p>
{{with .OK}}<p class="alert alert--ok" role="status">{{.}}</p>{{end}}
{{if .LicenseHealed}}<p class="alert alert--ok" role="status">A licen&ccedil;a da Evolution foi reativada automaticamente a partir da que este painel guardava, sem que ningu&eacute;m precisasse registrar de novo.</p>{{end}}

{{/* An instance the panel cannot even list is not one it can operate, so the
     controls for it stay out of the way until Evolution answers again. */}}
{{if and .Selected (not .Unavailable)}}
<section class="card">
<div class="card__head"><h2>Conta em uso: {{.SelectedName}}</h2>{{with .SessionLabel}}<span class="pill pill--{{$.SessionTone}}">{{.}}</span>{{end}}</div>
<div class="card__body stack">
<div class="hello"><span class="avatar" style="background:#128c7e">{{initial .AccountName}}</span><div><p class="hello__name">{{.AccountName}}</p>{{with .SelectedNumber}}<p class="hello__phone">{{phone .}}</p>{{end}}</div></div>
{{if .NeedsPairing}}<p class="note">A instância ainda não está pareada. Leia o QR code para conectar o WhatsApp.</p>
<div class="actions"><a class="btn" href="/pair">Ler o QR code</a></div>{{end}}
{{with .Status}}<dl class="facts">
<div class="fact"><dt>Último evento</dt><dd>{{relativeSince .LastEvent}}<span class="fact__detail">{{moment .LastEvent}}</span></dd></div>
{{if .HasIndex}}<div class="fact"><dt>Mensagens guardadas</dt><dd>{{count .Coverage.Messages}}</dd></div>
<div class="fact"><dt>Histórico desde</dt><dd>{{moment .Coverage.OldestAt}}</dd></div>{{end}}
</dl>{{end}}
</div></section>
{{end}}

<section class="card">
<div class="card__head"><h2>Instâncias</h2><a class="btn btn--ghost btn--small" href="#nova-instancia">Adicionar instância</a></div>
<div class="card__body">
{{if .Unavailable}}
{{if .NeedsActivation}}<div class="empty"><p class="empty__title">A licença ainda não foi ativada</p>
<p class="muted">Sem ela, a camada que mantém a sessão do WhatsApp não responde. É um e-mail e um clique, uma vez só.</p>
<div class="actions" style="justify-content:center;margin-top:14px"><a class="btn" href="/instalacao">Ativar a licença</a></div></div>
{{else}}<p class="alert" role="alert">{{.Notice}}</p>{{end}}
{{else if .Instances}}
<p class="muted">Uma instância é uma conta de WhatsApp conectada a este servidor.</p>
<form method="post" action="/instancias/selecionar">
<ul class="rows">
{{range $i, $inst := .Instances}}<li class="row{{if .Selected}} row--on{{end}}">
<label class="row__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if .Selected}} checked{{end}}>
<span class="row__main"><span class="row__title">{{.Name}}</span>{{if .Number}}<span class="row__meta">{{phone .Number}}</span>{{end}}</span>
</label>
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
{{if .Selected}}<span class="pill pill--ok pill--plain">Em uso pelo MCP</span>{{end}}
{{if .Managed}}<a class="row__remove" href="#remover-{{$i}}" title="Remover esta instância" aria-label="Remover a instância {{.Name}}"><span aria-hidden="true">✕</span></a>
{{else}}<span class="pill pill--off pill--plain">Sem credenciais aqui</span>{{end}}
</li>{{end}}
</ul>
<div class="actions" style="margin-top:14px"><button class="btn" type="submit">Usar a selecionada</button></div>
</form>
{{else}}
<div class="empty"><p class="empty__title">Nenhuma instância ainda</p><p class="muted">{{.Notice}}</p>
<div class="actions" style="justify-content:center;margin-top:14px"><a class="btn" href="#nova-instancia">Adicionar instância</a></div></div>
{{end}}
</div></section>

{{if and .Selected (not .Unavailable)}}
<section class="card">
<div class="card__head"><h2>Histórico</h2></div>
<div class="card__body stack">
<p class="muted">O índice cobre o que chegou desde que a instância foi conectada. Cada pedido traz mais histórico do celular para este servidor, e ele fica disponível para a sua ferramenta de IA consultar pelo MCP. O WhatsApp devolve mensagens anteriores a uma que ele já conhece, então cada pedido recua mais um trecho; o celular precisa estar com internet.</p>
<div class="actions"><form method="post" action="/instancias/historico"><button class="btn btn--ghost btn--small" type="submit">Buscar mensagens mais antigas</button></form></div>
</div></section>

<section class="card">
<div class="card__head"><h2>Conexão</h2></div>
<div class="card__body stack">
<div class="actions">
<form method="post" action="/instancias/conectar"><button class="btn btn--ghost btn--small" type="submit">Reconectar</button></form>
<form method="post" action="/instancias/desconectar"><button class="btn btn--ghost btn--small" type="submit">Desconectar</button></form>
</div>
<p class="muted">Desconectar apenas para o cliente e preserva o pareamento.</p>
</div></section>

<section class="card">
<div class="card__head"><h2>Zona de risco</h2></div>
<div class="card__body">
<div class="actions"><a class="btn btn--danger btn--small" href="#encerrar-sessao">Encerrar sessão do WhatsApp</a></div>
<p class="muted">O servidor sai da lista de dispositivos conectados do seu celular. As mensagens já indexadas continuam onde estão. Para apagar uma instância, use o ✕ na linha dela, na lista acima.</p>
</div></section>
{{end}}

<div class="overlay" id="nova-instancia" role="dialog" aria-modal="true" aria-labelledby="nova-instancia-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="nova-instancia-titulo">Adicionar instância</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<form method="post" action="/instancias" data-busy="Criando instância…">
<label class="field" for="new-instance"><span class="field__label">Nome da instância</span>
<span class="field__hint">Só para você identificar a conta. Ex.: “pessoal”, “trabalho”.</span></label>
<input id="new-instance" type="text" name="name" maxlength="60" required placeholder="pessoal" autocapitalize="none" spellcheck="false">
<p class="muted">Depois de criar, o painel abre o QR code para você parear o WhatsApp.</p>
<div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn" type="submit">Criar e parear</button></div>
<p class="busy" data-busy-note role="status" hidden>Criando a instância no WhatsApp. Isso leva alguns segundos; o painel abre o QR code assim que ela estiver pronta.</p>
</form>
</div></div></div>

<div class="overlay" id="encerrar-sessao" role="dialog" aria-modal="true" aria-labelledby="encerrar-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="encerrar-titulo">Encerrar a sessão do WhatsApp?</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<p>O pareamento é desfeito. Para voltar a usar esta instância será preciso ler um novo QR code no celular.</p>
<p class="muted">As mensagens já indexadas continuam onde estão.</p>
<form method="post" action="/instancias/sair"><div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn btn--danger" type="submit">Encerrar sessão</button></div></form>
</div></div></div>

{{range $i, $inst := .Instances}}{{if .Managed}}
<div class="overlay" id="remover-{{$i}}" role="dialog" aria-modal="true" aria-labelledby="remover-{{$i}}-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="remover-{{$i}}-titulo">Remover a instância?</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<div class="target">
<span class="target__name">{{.Name}}</span>
{{if .Number}}<span class="target__meta">{{phone .Number}}</span>{{else}}<span class="target__meta">Sem número: ainda não pareada.</span>{{end}}
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
</div>
<p>Esta instância é apagada e o WhatsApp é desconectado. As chaves de API emitidas para ela param de funcionar.</p>
<p class="muted">As mensagens já indexadas continuam no banco.</p>
<form method="post" action="/instancias/remover" data-busy="Removendo…">
<input type="hidden" name="instance_id" value="{{.ID}}">
<div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn btn--danger" type="submit">Remover instância</button></div>
<p class="busy" data-busy-note role="status" hidden>Removendo a instância no WhatsApp.</p>
</form>
</div></div></div>
{{end}}{{end}}
{{template "foot"}}{{end}}

{{define "estado"}}{{template "head" .}}{{template "nav" .}}
<h1>Status</h1>
<p class="lead">Se o seu WhatsApp está conectado e se as mensagens estão chegando a este servidor. É o mesmo retrato que as ferramentas do MCP e os endpoints de saúde reportam.</p>

{{with .Status}}
<section class="card">
<div class="card__head"><h2>WhatsApp</h2><span class="pill pill--{{sessionTone .WhatsApp.State}}">{{sessionLabel .WhatsApp.State}}</span></div>
<div class="card__body stack">
{{if .Problems}}<ul class="problems">{{range .Problems}}<li>{{.}}</li>{{end}}</ul>
{{else}}<p class="alert alert--ok">Nenhum problema detectado. As mensagens estão sendo recebidas e indexadas.</p>{{end}}
<dl class="facts">
<div class="fact"><dt>Último evento</dt><dd>{{relativeSince .LastEvent}}<span class="fact__detail">{{moment .LastEvent}}</span></dd></div>
{{if .WhatsApp.PushName}}<div class="fact"><dt>Conta</dt><dd>{{.WhatsApp.PushName}}</dd></div>{{end}}
{{if .HasIndex}}
<div class="fact"><dt>Mensagens indexadas</dt><dd>{{count .Coverage.Messages}}</dd></div>
<div class="fact"><dt>Histórico desde</dt><dd>{{moment .Coverage.OldestAt}}</dd></div>
{{end}}
</dl>
{{if .WhatsApp.Reason}}<p class="muted">Motivo informado pelo WhatsApp: <code>{{.WhatsApp.Reason}}</code></p>{{end}}
</div></section>

<section class="card">
<div class="card__head"><h2>Verificações</h2></div>
<div class="card__body">
<ul class="rows">
{{range .Checks}}<li class="row">
<span class="row__main"><span class="row__title">{{.Title}}</span><span class="row__meta">{{.Text}}</span></span>
<span class="pill pill--{{if eq .Status "ok"}}ok{{else if eq .Status "warn"}}warn{{else}}off{{end}}">{{if eq .Status "ok"}}Ok{{else if eq .Status "warn"}}Atenção{{else}}Problema{{end}}</span>
</li>{{end}}
</ul>
</div></section>

{{if .Queues}}
<section class="card">
<div class="card__head"><h2>Filas de ingestão</h2></div>
<div class="card__body">
<ul class="rows">
{{range .Queues}}<li class="row">
<span class="row__main"><span class="row__title mono">{{.Name}}</span><span class="row__meta">{{plural .Delivered "evento" "eventos"}}{{if .Rejected}} · {{plural .Rejected "rejeitado" "rejeitados"}}{{end}} · {{relativeSince .LastEventAt}}</span></span>
<span class="pill pill--{{if .Consuming}}ok{{else}}off{{end}}">{{if .Consuming}}Consumindo{{else}}Parada{{end}}</span>
</li>{{end}}
</ul>
<p class="muted">Uma fila parada acumula mensagens no broker sem que nada seja indexado.</p>
</div></section>
{{end}}
{{end}}

<section class="card">
<div class="card__head"><h2>Verificação externa</h2></div>
<div class="card__body">
<div class="actions"><a class="btn btn--ghost btn--small" href="/healthz">/healthz</a><a class="btn btn--ghost btn--small" href="/readyz">/readyz</a><a class="btn btn--ghost btn--small" href="/api/selected-instance">Instância selecionada</a></div>
</div></section>
{{template "foot"}}{{end}}

{{define "documentacao"}}{{template "head" .}}{{template "nav" .}}
<h1>O que o MCP sabe fazer</h1>
<p class="lead">As {{.Count}} funções que a sua ferramenta de IA pode usar no seu WhatsApp, lidas do próprio servidor: esta página só fica errada se o servidor estiver.</p>
<div class="tools">
{{range .Tools}}
<article class="tool">
<div class="tool__head"><span class="tool__name">{{.Name}}</span></div>
<p class="tool__desc">{{.Description}}</p>
{{if .Arguments}}<ul class="tool__args">
{{range .Arguments}}<li class="tool__arg"><span class="tool__argname">{{.Name}}</span><span class="tool__type">{{.Type}}</span>{{if .Required}}<span class="tool__req">obrigatório</span>{{end}}<span class="tool__argdesc">{{.Description}}{{if .Choices}} Valores: {{range $i, $c := .Choices}}{{if $i}}, {{end}}{{$c}}{{end}}.{{end}}</span></li>{{end}}
</ul>{{else}}<p class="tool__args muted">Sem argumentos.</p>{{end}}
</article>
{{end}}
</div>
{{template "foot"}}{{end}}

{{define "receitas"}}{{template "head" .}}{{template "nav" .}}
<h1>Receitas</h1>
<p class="lead">O WhatsApp MCP só responde pelo WhatsApp quando perguntado: esperar a hora, vigiar um termo e montar o relatório são trabalho da sua ferramenta de IA, escrito como instrução. Cada receita é um texto para colar.</p>
<div class="recipes">
{{range .Recipes}}
<article class="recipe">
<div class="recipe__head">
<h2 class="recipe__title">{{.Title}}</h2>
<p class="recipe__summary">{{.Summary}}</p>
</div>
<div class="recipe__meta">
{{range .Uses}}<span class="recipe__tool">{{.}}</span>{{end}}
{{with .Schedule}}<span class="recipe__tool recipe__tool--when">⏱ {{.}}</span>{{end}}
</div>
<div class="recipe__prompt">
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Prompt</span></div>
<pre data-copy><code>{{.Prompt}}</code></pre></div>
</div>
{{with .Caveat}}<p class="recipe__caveat"><strong>Atenção:</strong> {{.}}</p>{{end}}
</article>
{{end}}
</div>
{{template "foot"}}{{end}}

{{define "ajuda"}}{{template "head" .}}{{template "nav" .}}
<h1>Ajuda</h1>
<p class="lead">Respostas para as dúvidas mais comuns e exemplos para começar.</p>

<section class="card" id="perguntas">
<div class="card__head"><h2 class="card__title">{{icon "help"}}Perguntas frequentes</h2></div>
<div class="card__body">
<div class="faq">
<details class="faq__item"><summary>Funciona no Claude pelo navegador ou pelo celular?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Ainda não. O Claude no navegador e no celular, e o ChatGPT na web, só aceitam conectores que entram com login próprio, e este servidor usa uma chave de acesso por conexão. Funciona nos programas que aceitam essa chave, como o Claude Desktop, o Claude Code, o Codex e o Cursor, em qualquer computador: o servidor fica na internet.</p></div></details>
<details class="faq__item"><summary>A IA pode mandar mensagens sem eu pedir?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Não. Ler e procurar é seguro: mandar, reagir, encaminhar, editar ou apagar uma mensagem só acontece quando você pede. Apagar, sair de um grupo ou remover alguém pedem confirmação, e a IA pode mostrar um rascunho antes de enviar.</p></div></details>
<details class="faq__item"><summary>Onde ficam as minhas mensagens?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>No banco de dados deste servidor, que é seu: nada passa por um serviço de terceiros além do próprio WhatsApp. Quando você pede algo à sua ferramenta de IA, ela lê as mensagens de que precisa para responder, e o que ela faz com elas segue as regras de privacidade dela. A transcrição de áudio, se você ativar com uma chave da OpenAI, manda o áudio para a OpenAI.</p></div></details>
<details class="faq__item"><summary>O meu computador precisa ficar ligado?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Não. O servidor fica ligado o tempo todo e recebe as mensagens mesmo com o seu computador desligado. Se ele cair por um tempo, o WhatsApp entrega o que ficou pendente quando ele volta; se ainda faltar alguma coisa, peça o histórico ao celular na aba <a href="/whatsapp">WhatsApp</a>.</p></div></details>
<details class="faq__item"><summary>Como trazer mensagens mais antigas?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Na aba <a href="/whatsapp">WhatsApp</a>, use "Buscar mensagens mais antigas", ou peça à sua ferramenta de IA para buscar o histórico de uma conversa. Cada pedido traz mais histórico do celular para este servidor, e ele fica disponível para a sua ferramenta de IA consultar. O celular precisa estar com internet.</p></div></details>
<details class="faq__item"><summary>A IA consegue ouvir os meus áudios?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Sim. Com uma chave da OpenAI em <a href="/configuracoes#transcricao">Configurações › Transcrição de áudio</a>, o áudio vira texto com o Whisper quando você pede, e cada áudio é transcrito uma vez só. Se a sua ferramenta de IA roda comandos no seu computador, ela também pode baixar o áudio e transcrever ali mesmo, de graça, e guardar o texto aqui. Veja <a href="#transcrever">Transcrever áudios</a>, logo abaixo.</p></div></details>
<details class="faq__item"><summary>Posso conectar mais de uma ferramenta de IA?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Pode. Cada uma tem a sua chave, todas usam o mesmo WhatsApp, e desconectar uma não mexe nas outras. Para adicionar outra, use <a href="/conectar">Conectar ferramenta de IA ao MCP</a>.</p></div></details>
<details class="faq__item"><summary>A minha ferramenta não aparece conectada. E agora?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Feche a ferramenta e abra de novo, ou comece uma conversa nova: ela só carrega o WhatsApp ao iniciar. Confira se a chave foi colada inteira. Se continuar assim, crie uma conexão nova em <a href="/conectar">Conectar ferramenta de IA ao MCP</a> e confira a aba <a href="/status">Status</a>.</p></div></details>
<details class="faq__item"><summary>O que a IA consegue fazer no meu WhatsApp?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Ler, procurar e resumir conversas, dizer quem está esperando a sua resposta, transcrever áudios, mandar mensagens (respondendo, mencionando ou com rascunho antes), fotos, figurinhas, enquetes e localização, encaminhar, reagir, editar e apagar mensagens, organizar conversas e administrar grupos. A lista completa está em <a href="/funcoes">Funções</a>.</p></div></details>
<details class="faq__item"><summary>Como desconectar o WhatsApp deste servidor?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Na aba <a href="/whatsapp">WhatsApp</a>, em Zona de risco. O servidor sai dos dispositivos conectados do seu celular, e as mensagens já guardadas continuam aqui.</p></div></details>
<details class="faq__item"><summary>Onde ficam os meus dados?{{icon "chevron"}}</summary>
<div class="faq__answer"><p>Neste servidor, como aparece em <a href="/configuracoes#dados">Configurações › Dados</a>: as mensagens, as transcrições, as chaves e os webhooks no banco de dados, e os arquivos baixados numa pasta do servidor.</p></div></details>
</div>
</div></section>

<section class="card" id="como-usar">
<div class="card__head"><h2 class="card__title">{{icon "chat"}}Como usar</h2></div>
<div class="card__body stack">
<p class="muted">Escreva isso no chat da sua ferramenta de IA. Ler e procurar é seguro: mandar mensagem só acontece quando você pede.</p>
<ul class="prompts">
{{range .Prompts}}<li class="prompt"><div class="snippet"><pre data-copy><code>{{.}}</code></pre></div></li>{{end}}
</ul>
<div class="note"><p style="margin:0">Quer ir além? As <a href="/receitas">Receitas</a> trazem pedidos prontos para agendar mensagens, vigiar assuntos, resumir grupos e mais.</p></div>
</div></section>

<section class="card" id="transcrever">
<div class="card__head"><h2 class="card__title">{{icon "mic"}}Transcrever áudios</h2></div>
<div class="card__body stack">
<p class="muted">Com uma <a href="/configuracoes#transcricao">chave da OpenAI</a> salva, os áudios viram texto com o Whisper, só quando você pede. O áudio vai para a OpenAI e é cobrado na conta dessa chave (<a href="{{.Pricing}}" rel="noopener noreferrer" target="_blank">US$ {{.PricePerMinute}} por minuto</a>).</p>
<p class="muted">Se a sua ferramenta de IA roda comandos no seu computador e ele tem uma placa que o Whisper aproveite (um Mac com Apple Silicon, ou uma NVIDIA), ela pode baixar o áudio, transcrever ali mesmo, de graça e sem o áudio sair de lá, e guardar o texto aqui.</p>
<p class="muted">Peça algo como a mensagem abaixo. Os áudios já transcritos vêm junto das mensagens; os outros ela transcreve na hora.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Exemplo</span></div>
<pre class="plain" data-copy><code>Transcreva os áudios que recebi hoje no WhatsApp.</code></pre></div>
<p class="muted" style="margin:0">Cada áudio é transcrito uma vez: pedir de novo devolve o texto guardado, sem nova cobrança.</p>
</div></section>
{{template "foot"}}{{end}}

{{define "configuracoes"}}{{template "head" .}}{{template "nav" .}}
<h1>Configurações</h1>
<p class="lead">Como o WhatsApp MCP roda neste servidor.</p>
{{with .OK}}<p class="alert alert--ok" role="status">{{.}}</p>{{end}}

<section class="card" id="endereco">
<div class="card__head"><h2 class="card__title">{{icon "link"}}Endereço do MCP</h2></div>
<div class="card__body stack">
<p class="muted">As ferramentas de IA falam com o WhatsApp MCP por este endereço, cada uma com a chave de acesso da sua conexão. Ele não é segredo: o segredo é a chave, que aparece uma única vez, quando a conexão é criada.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Endereço</span></div><pre data-copy><code>{{.Endpoint}}</code></pre></div>
<p class="muted" style="margin:0">Ele vem da variável <code>PUBLIC_URL</code> do servidor. Trocar o domínio exige configurar as ferramentas de novo.</p>
</div></section>

<section class="card" id="conta">
<div class="card__head"><h2 class="card__title">{{icon "user"}}Conta</h2></div>
<div class="card__body stack">
<p class="muted">Você entra neste painel como <strong>{{.Admin}}</strong>.</p>
<div class="actions"><a class="btn btn--ghost btn--small" href="/senha">Trocar a senha</a>
<form method="post" action="/logout"><button class="btn btn--quiet btn--small" type="submit">Sair</button></form></div>
</div></section>

<section class="card" id="webhooks" data-webhooks>
<div class="card__head"><h2 class="card__title">{{icon "code"}}Webhooks</h2><a class="btn btn--ghost btn--small" href="/webhooks/documentacao">{{icon "book"}}Documentação</a></div>
<div class="card__body stack">
<p class="muted" data-webhooks-intro hidden>Avisam um programa seu a cada mensagem nova que chega, para ele agir sozinho: responder, registrar numa planilha, avisar em outro lugar. São para quem usa scripts ou automações. <span class="tip" tabindex="0" aria-describedby="tip-webhooks"><span aria-hidden="true">?</span><span class="tip__body" role="tooltip" id="tip-webhooks">A cada mensagem, este servidor faz um POST com JSON no endereço, assinado com a chave do webhook. Se o endereço não responder com sucesso, ele tenta de novo até 10 vezes em menos de um minuto; depois disso o webhook é desligado e os avisos que esperavam são descartados. O formato de cada aviso está em Documentação, no alto deste card.</span></span></p>
<p class="note" data-webhooks-unavailable hidden>Os webhooks não estão disponíveis neste servidor.</p>
<p class="busy" data-webhooks-loading role="status"><span class="spinner" aria-hidden="true"></span>Carregando…</p>
<ul class="rows" data-webhooks-list hidden></ul>
<div class="empty" data-webhooks-empty hidden><p class="empty__title">Nenhum webhook configurado</p><p class="muted" style="margin:6px 0 12px">Avisam um programa seu a cada mensagem nova, para quem usa scripts ou automações.</p><button class="btn btn--ghost btn--small" type="button" data-webhook-start>Configurar o primeiro webhook</button></div>
<div class="secret" data-webhook-secret hidden>
<p class="secret__title">Webhook adicionado. Guarde a chave dele</p>
<p class="muted" style="margin:0">Ela não aparece de novo. Com ela, o seu programa confere que cada aviso veio mesmo do WhatsApp MCP.</p>
<code class="secret__value" data-webhook-secret-value></code>
<div class="actions"><button class="btn btn--ghost btn--small" type="button" data-webhook-secret-copy>Copiar a chave</button><button class="btn btn--quiet btn--small" type="button" data-webhook-secret-close>Já guardei</button></div>
</div>
<form class="stack" data-webhook-form hidden>
<h3 class="card__sub">Adicionar um webhook</h3>
<label class="field" for="webhook-url"><span class="field__label">Endereço</span><span class="field__hint">O endereço do seu programa, começando com http:// ou https://. Precisa ser alcançável a partir deste servidor.</span></label>
<input id="webhook-url" type="text" inputmode="url" autocomplete="off" spellcheck="false" placeholder="https://exemplo.com/whatsapp" required data-webhook-url>
<span class="field__label">Avisar quando</span>
<label class="check"><input type="checkbox" value="message" checked data-webhook-event><span>Chegar uma mensagem</span></label>
<label class="check"><input type="checkbox" value="reaction" data-webhook-event><span>Alguém reagir a uma mensagem</span></label>
<label class="check"><input type="checkbox" value="receipt" data-webhook-event><span>Uma mensagem sua for entregue, lida ou ouvida</span></label>
<label class="check"><input type="checkbox" data-webhook-own><span>Incluir as mensagens que você manda<span class="check__hint">Do celular ou pela sua ferramenta de IA.</span></span></label>
<div class="actions"><button class="btn btn--small" type="submit">Adicionar webhook</button><button class="btn btn--quiet btn--small" type="button" data-webhook-cancel hidden>Cancelar</button></div>
<p class="alert" role="alert" data-webhook-error hidden></p>
</form>
</div></section>

<section class="card" id="transcricao">
<div class="card__head"><h2 class="card__title">{{icon "mic"}}Transcrição de áudio</h2>{{if not .Transcription}}<span class="pill pill--off">Indisponível</span>{{else if .Status.Configured}}<span class="pill pill--ok">Ativa</span>{{else}}<span class="pill pill--off">Não configurada</span>{{end}}</div>
<div class="card__body stack">
{{if not .Transcription}}
<p class="muted">A transcrição não está disponível neste servidor.</p>
{{else if .Status.Configured}}
<p class="muted">Os áudios viram texto com o Whisper da OpenAI, só quando você pede, e são cobrados na conta desta chave.</p>
<dl class="facts">
<div class="fact"><dt>Chave salva</dt><dd class="mono">{{.Status.Hint}}</dd></div>
<div class="fact"><dt>Salva em</dt><dd>{{moment .Status.UpdatedAt}}</dd></div>
</dl>
<div class="actions">
<a class="btn btn--ghost btn--small" href="{{.Links.Usage}}" rel="noopener noreferrer" target="_blank">Uso e custos ↗</a>
<a class="btn btn--ghost btn--small" href="{{.Links.Billing}}" rel="noopener noreferrer" target="_blank">Créditos ↗</a>
<a class="btn btn--ghost btn--small" href="{{.Links.Limits}}" rel="noopener noreferrer" target="_blank">Limite de gastos ↗</a>
</div>
<details class="disclose"><summary>Trocar ou remover a chave</summary>
<div class="stack">
{{template "transcricaoform" .}}
<form method="post" action="/transcricao/remover">
<div class="actions"><button class="btn btn--danger btn--small" type="submit">Remover chave</button></div>
</form>
</div></details>
{{else}}
<p class="muted">Com uma chave da sua conta na OpenAI, a sua ferramenta de IA transcreve os áudios que você recebe usando o Whisper. Os áudios são cobrados nela, e só nela: US$&nbsp;{{.PricePerMinute}} por minuto.</p>
<details class="disclose"><summary>Como conseguir uma chave</summary>
<ol class="guide">
<li><strong>Crie uma conta na plataforma da OpenAI.</strong> É a plataforma de desenvolvedores, separada do ChatGPT: uma assinatura do ChatGPT Plus não inclui créditos para a API. <a href="{{.Links.Signup}}" rel="noopener noreferrer" target="_blank">Criar conta ↗</a></li>
<li><strong>Adicione créditos.</strong> Em <em>Billing</em>, o mínimo é US$&nbsp;5, que dão para cerca de 800 minutos de áudio. <a href="{{.Links.Billing}}" rel="noopener noreferrer" target="_blank">Adicionar créditos ↗</a></li>
<li><strong>Crie a chave de API.</strong> Em <em>API keys</em>, clique em <em>Create new secret key</em> e copie a chave na hora: a OpenAI só mostra ela uma vez. <a href="{{.Links.Keys}}" rel="noopener noreferrer" target="_blank">Criar chave ↗</a></li>
<li><strong>Cole a chave aqui embaixo e salve.</strong> Ela é conferida com a OpenAI antes de ser guardada.</li>
</ol>
</details>
{{template "transcricaoform" .}}
{{end}}
<p class="muted" style="margin:0"><a href="/ajuda#transcrever">Como usar</a>, e como transcrever de graça no seu computador.</p>
</div></section>

<section class="card" id="atualizacoes">
<div class="card__head"><h2 class="card__title">{{icon "refresh"}}Versão e atualizações</h2></div>
<div class="card__body stack">
<dl class="facts"><div class="fact"><dt>Versão instalada</dt><dd>{{.Version}}</dd></div><div class="fact"><dt>Mais recente</dt><dd>{{with .Latest}}{{.}}{{else}}—{{end}}</dd></div></dl>
{{if .Behind}}
{{if .CanSelfUpdate}}{{if .UpdateBusy}}<p class="muted"><a href="/atualizacao">Atualização em andamento →</a></p>
{{else}}<form method="post" action="/atualizar" data-busy="Pedindo…"><input type="hidden" name="version" value="{{.Latest}}"><div class="actions"><button class="btn btn--small" type="submit">Atualizar para a {{.Latest}}</button><a class="btn btn--quiet btn--small" href="{{releaseURL .Latest}}" rel="noopener noreferrer" target="_blank">Ver o que mudou</a></div></form>
<p class="muted">O servidor faz backup do banco antes de tudo e reinicia o painel no fim — você vai precisar entrar de novo.</p>{{end}}
{{else}}<p class="muted">Rode na sua instância, por SSH. Os segredos, o pareamento e as mensagens indexadas são preservados.</p>
<div class="snippet"><pre data-copy><code>{{updateCommand}}</code></pre></div>{{end}}
{{else if .Latest}}<p class="muted">Você já tem a versão mais recente.</p>
{{else}}<p class="muted">O servidor ainda não conferiu se há uma versão nova. Quando houver, o aviso aparece no topo de todas as páginas.</p>{{end}}
</div></section>

<section class="card" id="aparencia" hidden data-theme-switch>
<div class="card__head"><h2 class="card__title">{{icon "palette"}}Aparência</h2></div>
<div class="card__body">
<fieldset class="themes"><legend class="sr-only">Tema</legend>
<label class="theme-pick"><input type="radio" name="tema" value="light" data-theme-choice><span class="theme-pick__preview" aria-hidden="true"><span class="mini mini--light"><i></i><i></i><i></i></span></span><span class="theme-pick__label">{{icon "sun"}}Claro</span></label>
<label class="theme-pick"><input type="radio" name="tema" value="dark" data-theme-choice><span class="theme-pick__preview" aria-hidden="true"><span class="mini mini--dark"><i></i><i></i><i></i></span></span><span class="theme-pick__label">{{icon "moon"}}Escuro</span></label>
<label class="theme-pick"><input type="radio" name="tema" value="system" data-theme-choice><span class="theme-pick__preview" aria-hidden="true"><span class="mini mini--light"><i></i><i></i><i></i></span><span class="mini mini--dark mini--half"><i></i><i></i><i></i></span></span><span class="theme-pick__label">{{icon "monitor"}}Sistema</span></label>
</fieldset>
<p class="muted" style="margin:12px 0 0">Vale só para este navegador.</p>
</div></section>

<section class="card" id="arquivos" data-media>
<div class="card__head"><h2 class="card__title">{{icon "folder"}}Arquivos baixados</h2></div>
<div class="card__body stack">
<p class="muted">Fotos, áudios e documentos que as suas ferramentas de IA abriram ficam guardados neste servidor, para não serem baixados de novo, e continuam legíveis mesmo depois que o WhatsApp os descarta. Apagar não perde nenhuma mensagem: se precisar, o arquivo é baixado outra vez, enquanto o WhatsApp ainda o tiver.</p>
<dl class="facts">
<div class="fact"><dt>Espaço usado</dt><dd data-media-bytes>…</dd></div>
<div class="fact"><dt>Arquivos</dt><dd data-media-files>…</dd></div>
<div class="fact"><dt>Exportações</dt><dd data-media-exports>…</dd></div>
</dl>
<p class="muted" data-media-types hidden></p>
<div class="actions"><label class="check check--inline"><input type="checkbox" data-media-retention><span>Apagar sozinho os arquivos baixados há mais de</span></label><input class="input--short" type="number" min="1" max="3650" value="30" aria-label="dias" data-media-days disabled><span class="muted">dias</span></div>
<p class="muted" style="margin:0">As exportações de conversas ficam até você apagar. <span class="tip" tabindex="0" aria-describedby="tip-exportacoes"><span aria-hidden="true">?</span><span class="tip__body" role="tooltip" id="tip-exportacoes">Quando você pede à sua ferramenta de IA para exportar conversas, o servidor grava um arquivo com as mensagens e entrega um link para ela baixar, para analisar sem carregar tudo na conversa.</span></span></p>
<div class="actions"><button class="btn btn--ghost btn--small" type="button" data-media-clear hidden>Apagar os arquivos baixados</button><button class="btn btn--ghost btn--small" type="button" data-media-clear-exports hidden>Apagar as exportações</button></div>
<p class="busy" data-media-note role="status" hidden></p>
</div></section>
<script src="/assets/settings.js" defer></script>

<section class="card" id="dados">
<div class="card__head"><h2 class="card__title">{{icon "database"}}Dados</h2></div>
<div class="card__body stack">
<p class="muted">Tudo fica neste servidor. As mensagens indexadas, as transcrições, as chaves das conexões e os webhooks ficam no banco de dados PostgreSQL do WhatsApp MCP; a sessão do WhatsApp, na Evolution; os arquivos baixados e as exportações, nas pastas abaixo, no volume de dados do servidor.</p>
{{with .DataDir}}<div class="snippet"><div class="snippet__head"><span class="snippet__title">Arquivos baixados</span></div><pre data-copy><code>{{.}}</code></pre></div>{{end}}
{{with .ExportsDir}}<div class="snippet"><div class="snippet__head"><span class="snippet__title">Exportações</span></div><pre data-copy><code>{{.}}</code></pre></div>{{end}}
<p class="muted" style="margin:0">O banco é copiado antes de cada atualização. Para tirar o WhatsApp deste servidor, use a Zona de risco na aba <a href="/whatsapp">WhatsApp</a>.</p>
</div></section>
{{template "foot"}}{{end}}


{{define "updatebanner"}}{{$page := .}}{{with rolledBackFrom}}
<div class="update update--rollback" role="alert">
<p class="update__line">Esta instância voltou para a versão {{version}}, mas o banco de dados já rodou a <span class="update__version">{{.}}</span>.</p>
<p class="update__note">As migrações só andam para frente: uma versão mais antiga pode não entender o que a {{.}} gravou, e falhar em algum canto. Volte para a {{.}} ou uma mais nova.</p>
{{if $page.CanSelfUpdate}}{{if $page.UpdateBusy}}<p class="update__note"><a href="/atualizacao">Atualização em andamento →</a></p>{{else}}<form method="post" action="/atualizar" data-busy="Pedindo…"><input type="hidden" name="version" value="{{.}}"><div class="actions" style="margin-top:10px"><button class="btn btn--small" type="submit">Voltar para a {{.}}</button></div></form>{{end}}{{end}}
</div>
{{else}}{{with newRelease}}
<div class="update">
<p class="update__line">Versão <span class="update__version">{{.}}</span> disponível — esta instância roda a {{version}}.
<a class="update__notes" href="{{releaseURL .}}" rel="noopener noreferrer" target="_blank">Ver o que mudou</a></p>
{{if $page.CanSelfUpdate}}{{if $page.UpdateBusy}}<p class="update__note"><a href="/atualizacao">Atualização em andamento →</a></p>{{else}}<form method="post" action="/atualizar" data-busy="Pedindo…"><input type="hidden" name="version" value="{{.}}"><div class="actions"><button class="btn btn--small" type="submit">Atualizar para a {{.}}</button></div></form>
<p class="update__note">O servidor faz backup do banco antes de tudo e reinicia o painel no fim — você vai precisar entrar de novo. Os segredos, o pareamento e as mensagens indexadas são preservados.</p>{{end}}
{{else}}<div class="snippet"><pre data-copy><code>{{updateCommand}}</code></pre></div>
<p class="update__note">Rode na sua instância, por SSH. Os segredos, o pareamento e as mensagens indexadas são preservados; o banco é copiado antes de qualquer migração.</p>{{end}}
</div>
{{end}}{{end}}{{end}}

{{define "pair"}}{{template "head" .}}{{template "nav" .}}
<h1>Conectar o WhatsApp</h1>
{{with .Name}}<p class="lead">Instância <strong>{{.}}</strong>.</p>{{end}}

<section class="card">
<div class="card__head"><h2>QR code</h2><span class="pill pill--warn">Aguardando leitura</span></div>
<div class="card__body stack">
{{if .QRCode}}
<ol class="guide">
<li>Abra o <strong>WhatsApp</strong> no celular.</li>
<li>Toque em <strong>Configurações</strong> (no Android, o menu <strong>⋮</strong>).</li>
<li>Toque em <strong>Dispositivos conectados</strong>.</li>
<li>Toque em <strong>Conectar um dispositivo</strong>.</li>
<li>Aponte a câmera para o QR code abaixo.</li>
</ol>
<img class="qrcode" src="{{.QRCode}}" alt="QR code para conectar o WhatsApp" width="250" height="250">
{{else}}
<div class="empty"><p class="empty__title">Aguardando o QR code</p><p class="muted">{{.Notice}}</p></div>
{{end}}
<p class="muted">Esta página se atualiza sozinha a cada 5 segundos. O código expira rápido; se sumir, gere outro.</p>
<div class="actions">
<form method="post" action="/instancias/conectar"><button class="btn btn--ghost" type="submit">Gerar outro código</button></form>
<a class="btn btn--quiet" href="/instancias">Voltar</a>
</div>
</div></section>
{{template "foot"}}{{end}}

{{define "licencaform"}}<form method="post" action="/instancias/licenca" data-busy="Enviando…">
<label class="field" for="license-email"><span class="field__label">E-mail para a licença</span>
<span class="field__hint">Voc&ecirc; recebe um link de ativação neste endereço, v&aacute;lido por 15 minutos. Não &eacute; o e-mail com que voc&ecirc; entra no painel: a Evolution Foundation registra uma licença por endereço, então use um que ainda não tenha licenciado outra instalação.</span></label>
<input id="license-email" type="email" name="email" maxlength="254" required placeholder="voce@exemplo.com" autocomplete="off" autocapitalize="none" spellcheck="false">
<div class="actions"><button class="btn btn--ghost" type="submit">Enviar link de ativação</button></div>
<p class="busy" data-busy-note role="status" hidden>Pedindo o link ao servidor de licenças.</p>
</form>{{end}}

{{define "instalacao"}}{{template "head" .}}
<header class="masthead"><a class="brand" href="/">{{template "brandmark"}}</a>
<div class="masthead__tools">{{template "settingslink" .}}</div></header>
<div class="wizard-shell"{{if and (eq .Step 1) .Sent}} data-onboarding="1"{{end}}>
<ol class="wizard" aria-label="Etapas da instalação">
{{range .Steps}}<li class="wizard__step wizard__step--{{.State}}"{{if eq .State "now"}} aria-current="step"{{end}}>
<span class="wizard__n" aria-hidden="true">{{if eq .State "done"}}✓{{else}}{{.Number}}{{end}}</span><span class="wizard__label">{{.Label}}</span></li>{{end}}
</ol>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{with .OK}}<p class="alert alert--ok" role="status">{{.}}</p>{{end}}

{{if eq .Step 1}}
<section class="card">
<div class="card__head"><h2>Ativar a licença</h2></div>
<div class="card__body">
{{if and .Auto .AutoStalled}}
<p class="lead">A ativação automática não se completou.</p>
<p class="muted">Ela {{if .Sent}}não respondeu no tempo esperado{{else}}não chegou a sair{{end}}, e esperar mais não resolve. O caminho agora &eacute; uma caixa de entrada que voc&ecirc; abra: informe um endereço, clique no link que chegar, e a licença entra do mesmo jeito.</p>
{{template "licencaform" .}}
<details class="resend">
<summary>Tentar a automática de novo</summary>
<p class="muted">Recomeça a ativação automática, o que vale a pena se a falha foi passageira.</p>
<form method="post" action="/instancias/licenca/auto"><input type="hidden" name="origem" value="instalacao">
<div class="actions"><button class="btn btn--ghost" type="submit">Tentar a ativação automática</button></div>
</form>
{{if .RegisterURL}}<p class="muted">Ou faça no site da Evolution: <a href="{{.RegisterURL}}" rel="noopener noreferrer" target="_blank">abrir o registro</a> — leva ao mesmo lugar.</p>{{end}}
</details>
{{else if and .Auto .Sent}}
<p class="lead">Verificando a licença do software.</p>
<p class="muted">A ativação &eacute; automática e não pede nada de voc&ecirc;. Esta página segue sozinha assim que a licença entrar.</p>
<p class="busy" role="status"><span class="spinner" aria-hidden="true"></span>Ativando…</p>
<details class="resend">
<summary>Prefiro ativar manualmente</summary>
{{template "licencaform" .}}
</details>
{{else if .Sent}}
<p class="lead">Enviamos um link de ativação{{with .OperatorEmail}} para <strong>{{.}}</strong>{{end}}.</p>
<p class="muted">Abra o e-mail e clique no link. Ele vale por 15 minutos, e esta página segue sozinha assim que a licença entrar.</p>
<p class="busy" role="status"><span class="spinner" aria-hidden="true"></span>Verificando a ativação…</p>
<details class="resend">
<summary>Não recebeu o e-mail?</summary>
<form method="post" action="/instancias/licenca" data-busy="Enviando…">
<label class="field" for="license-email-resend"><span class="field__label">Enviar para outro endereço</span></label>
<input id="license-email-resend" type="email" name="email" maxlength="254" required{{with .OperatorEmail}} value="{{.}}"{{end}} placeholder="voce@exemplo.com" autocomplete="email" autocapitalize="none" spellcheck="false">
<div class="actions"><button class="btn btn--ghost" type="submit">Enviar de novo</button></div>
</form>
{{if .RegisterURL}}<p class="muted">Prefere fazer no site da Evolution? <a href="{{.RegisterURL}}" rel="noopener noreferrer" target="_blank">Abrir o registro</a> — leva ao mesmo lugar.</p>
{{else}}<p class="muted">O link de registro não veio agora. Ele tamb&eacute;m sai no servidor com:</p>
<pre><code>whatsapp-mcp logs evolution-go | grep -i license</code></pre>{{end}}
</details>
{{else}}
<p class="lead">Este servidor usa a Evolution Go para manter a sessão do WhatsApp, e ela pede uma licença gratuita. &Eacute; uma vez s&oacute;.</p>
{{if .Auto}}
<p class="muted">O pedido de ativação automática ainda não saiu — normalmente porque a Evolution acabou de subir e ainda não aceita registros. Esta página tenta sozinha a cada poucos segundos.</p>
<form method="post" action="/instancias/licenca/auto"><input type="hidden" name="origem" value="instalacao">
<div class="actions"><button class="btn btn--ghost" type="submit">Tentar agora</button></div>
</form>
{{else}}
<form method="post" action="/instancias/licenca" data-busy="Enviando…">
<label class="field" for="license-email"><span class="field__label">Seu e-mail</span>
<span class="field__hint">Voc&ecirc; recebe um link de ativação neste endereço.</span></label>
<input id="license-email" type="email" name="email" maxlength="254" required placeholder="voce@exemplo.com" autocomplete="email" autocapitalize="none" spellcheck="false"{{with .OperatorEmail}} value="{{.}}"{{end}}>
<div class="actions"><button class="btn btn--block" type="submit">Enviar link de ativação</button></div>
<p class="busy" data-busy-note role="status" hidden>Pedindo o link ao servidor de licenças.</p>
</form>
{{end}}
{{end}}
</div></section>
{{end}}

{{if eq .Step 2}}
<section class="card">
<div class="card__head"><h2>Conectar o WhatsApp</h2></div>
<div class="card__body">
{{if .Unavailable}}
<p class="lead">{{.Notice}}</p>
<p class="muted">Esta página tenta de novo sozinha a cada 5 segundos.</p>
{{else if .NeedsPairing}}
<ol class="guide">
<li>Abra o <strong>WhatsApp</strong> no celular.</li>
<li>Toque em <strong>Configurações</strong> (no Android, o menu <strong>⋮</strong>).</li>
<li>Toque em <strong>Dispositivos conectados</strong>, depois em <strong>Conectar um dispositivo</strong>.</li>
<li>Aponte a câmera para o código abaixo.</li>
</ol>
{{if .QRCode}}<img class="qrcode" src="{{.QRCode}}" alt="QR code para conectar o WhatsApp" width="250" height="250">
{{else}}<div class="empty"><p class="empty__title">Aguardando o QR code</p><p class="muted">{{.QRNotice}}</p></div>{{end}}
<p class="muted">O código expira rápido; esta página busca outro sozinha.</p>
<div class="actions"><form method="post" action="/instancias/conectar"><input type="hidden" name="origem" value="instalacao"><button class="btn btn--ghost btn--small" type="submit">Gerar outro código</button></form></div>
{{else if .Instances}}
<p class="lead">Escolha qual conta de WhatsApp o MCP deve usar.</p>
<form method="post" action="/instancias/selecionar">
<input type="hidden" name="origem" value="instalacao">
<ul class="rows">
{{range .Instances}}<li class="row{{if .Selected}} row--on{{end}}">
<label class="row__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if .Selected}} checked{{end}}>
<span class="row__main"><span class="row__title">{{.Name}}</span>{{if .Number}}<span class="row__meta">{{phone .Number}}</span>{{end}}</span>
</label>
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
</li>{{end}}
</ul>
<div class="actions" style="margin-top:14px"><button class="btn btn--block" type="submit">Usar esta conta</button></div>
</form>
{{else}}
<p class="lead">Dê um nome para esta conta de WhatsApp. É só um rótulo para você reconhecer depois.</p>
<form method="post" action="/instancias" data-busy="Criando…">
<input type="hidden" name="origem" value="instalacao">
<label class="field" for="new-instance"><span class="field__label">Nome</span></label>
<input id="new-instance" type="text" name="name" maxlength="60" required value="pessoal" autocapitalize="none" spellcheck="false">
<div class="actions"><button class="btn btn--block" type="submit">Criar e gerar o QR code</button></div>
<p class="busy" data-busy-note role="status" hidden>Criando a conta no WhatsApp. Isso leva alguns segundos; o QR code aparece assim que ela estiver pronta.</p>
</form>
{{end}}
</div></section>
{{end}}

{{if eq .Step 3}}
<section class="card card--accent">
<div class="card__head"><h2>Tudo pronto</h2><span class="pill pill--ok">WhatsApp conectado</span></div>
<div class="card__body">
<p class="lead">Falta ligar uma ferramenta de IA a este MCP. O passo a passo de cada uma vem com a chave e o comando já preenchidos.</p>
<div class="actions"><a class="btn btn--block" href="/conectar">Conectar ferramenta de IA ao MCP</a></div>
<div class="wizard__escape"><a class="btn btn--quiet" href="/">Ir para o painel</a></div>
</div></section>
{{end}}

{{if ne .Step 3}}<div class="wizard__escape"><form method="post" action="/logout"><button class="btn btn--quiet" type="submit">Sair</button></form></div>{{end}}
</div>
{{template "foot"}}{{end}}

{{define "setup"}}{{template "head" .}}
<div style="max-width:460px;margin:0 auto;padding-top:8vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
<section class="card">
<div class="card__head"><h2>Configuração inicial</h2></div>
<div class="card__body">
{{if .Locked}}<p class="lead">Esta p&aacute;gina s&oacute; abre pelo link que o instalador imprimiu no fim da instala&ccedil;&atilde;o, com o token no fim dele.</p>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<p class="muted">Perdeu o link? Ele pode ser remontado no servidor:</p>
<pre><code>echo "$(cat /opt/whatsapp-mcp/hostname | sed 's|^|https://|')/setup?token=$(sed -n 's/^SETUP_TOKEN=//p' /opt/whatsapp-mcp/.env)"</code></pre>
{{else}}<p class="lead">Crie o único administrador deste painel. Depois disso esta página deixa de aceitar cadastros.</p>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<form method="post">
{{with .Token}}<input type="hidden" name="setup_token" value="{{.}}">{{end}}
<label class="field" for="email"><span class="field__label">E-mail</span>
<span class="field__hint">Use um e-mail válido e que você consiga abrir: o próximo passo pede uma confirmação nele.</span></label>
<input id="email" type="email" name="email" maxlength="254" required placeholder="voce@exemplo.com" autocomplete="username" autocapitalize="none" spellcheck="false"{{with .Email}} value="{{.}}"{{end}}>
<label class="field" for="password"><span class="field__label">Senha</span>
<span class="field__hint">Mínimo de 6 caracteres.</span></label>
<input id="password" type="password" name="password" minlength="6" required autocomplete="new-password">
<div class="actions"><button class="btn btn--block" type="submit">Criar administrador</button></div>
</form>{{end}}
</div></section>
<p class="muted" style="text-align:center">As senhas são guardadas com bcrypt e nunca aparecem nos logs.</p>
</div>
{{template "foot"}}{{end}}

{{define "licenca"}}{{template "head" .}}
<div style="max-width:460px;margin:0 auto;padding-top:8vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
<section class="card">
<div class="card__head"><h2>{{if .OK}}Licença ativada{{else}}A ativação não foi concluída{{end}}</h2></div>
<div class="card__body">
{{if .OK}}<p class="lead">{{.OK}}</p>
<p class="muted">Pode fechar esta aba: a instalação já seguiu sozinha na aba onde você começou.</p>
{{else}}<p class="alert" role="alert">{{.Reason}}</p>
<p class="muted">Volte ao painel e peça outro link em <strong>Não recebeu o e-mail?</strong>. Cada link vale por 15 minutos e só pode ser usado uma vez.</p>{{end}}
<div class="actions"><a class="btn btn--block" href="/instalacao">Abrir o painel</a></div>
</div></section>
</div>
{{template "foot"}}{{end}}

{{define "senha"}}{{template "head" .}}
{{if .Forced}}<div style="max-width:460px;margin:0 auto;padding-top:8vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
{{else}}{{template "nav" .}}<div style="max-width:460px;margin:0 auto">{{end}}
<section class="card">
<div class="card__head"><h2>{{if .Forced}}Defina uma senha{{else}}Trocar a senha{{end}}</h2></div>
<div class="card__body">
{{if .Forced}}<p class="lead">A senha atual foi gerada pelo instalador e apareceu no terminal. Escolha uma sua antes de continuar.</p>
{{else}}<p class="lead">A troca vale imediatamente. As outras sess&otilde;es continuam abertas at&eacute; o pr&oacute;ximo reinicio do servi&ccedil;o.</p>{{end}}
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{with .Saved}}<p class="ok" role="status">{{.}}</p>{{end}}
<form method="post">
<label class="field" for="current"><span class="field__label">Senha atual</span></label>
<input id="current" type="password" name="current_password" required autocomplete="current-password">
<label class="field" for="password"><span class="field__label">Nova senha</span>
<span class="field__hint">Mínimo de 6 caracteres.</span></label>
<input id="password" type="password" name="password" minlength="6" required autocomplete="new-password">
<label class="field" for="confirm"><span class="field__label">Repita a nova senha</span></label>
<input id="confirm" type="password" name="confirm_password" minlength="6" required autocomplete="new-password">
<div class="actions"><button class="btn btn--block" type="submit">Salvar senha</button></div>
</form>
</div></section>
{{if .Forced}}<p class="muted" style="text-align:center">A senha do instalador continua v&aacute;lida at&eacute; esta troca. Nada mais do painel abre antes dela.</p>{{end}}
</div>
{{template "foot"}}{{end}}

{{define "login"}}{{template "head" .}}
<div style="max-width:460px;margin:0 auto;padding-top:8vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
<section class="card">
<div class="card__head"><h2>Entrar</h2></div>
<div class="card__body">
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<form method="post">
<label class="field" for="username"><span class="field__label">E-mail</span></label>
<input id="username" type="text" inputmode="email" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha</span></label>
<input id="password" type="password" name="password" required autocomplete="current-password">
<div class="actions"><button class="btn btn--block" type="submit">Entrar</button></div>
</form>
</div></section>
</div>
{{template "foot"}}{{end}}

{{define "transcricaoform"}}<form method="post" action="/transcricao" data-busy="Verificando…">
<label class="field" for="api_key"><span class="field__label">{{if .Status.Configured}}Nova chave{{else}}Chave de API{{end}}</span>
<span class="field__hint">Começa com <code>sk-</code>. Depois de salva, ela nunca mais aparece inteira.</span></label>
<input id="api_key" type="password" name="api_key" required placeholder="sk-…" autocomplete="off" spellcheck="false">
<div class="actions"><button class="btn" type="submit">Salvar chave</button></div>
</form>{{end}}

{{define "atualizacao"}}{{template "head" .}}{{template "nav" .}}
<h1>Atualização</h1>
<section class="card card--accent" data-update-status data-target="{{.Target}}" data-running="{{.Running}}">
<div class="card__head"><h2>{{if .Target}}Para a versão {{.Target}}{{else}}Nenhuma atualização pedida{{end}}</h2>
{{if .Has}}<span class="pill pill--{{if eq .Status.State "succeeded"}}ok{{else if eq .Status.State "failed"}}off{{else}}warn{{end}}" data-update-state>{{if eq .Status.State "queued"}}Na fila{{else if eq .Status.State "running"}}Em andamento{{else if eq .Status.State "succeeded"}}Concluída{{else}}Falhou{{end}}</span>{{end}}</div>
<div class="card__body stack">
{{if .Has}}
<p class="lead" data-update-message>{{if .Status.Message}}{{.Status.Message}}{{else if eq .Status.State "queued"}}Pedido enviado. O agente do servidor pega em alguns segundos.{{else if eq .Status.State "running"}}{{if .Status.Phase}}{{.Status.Phase}}{{else}}Atualizando…{{end}}{{else if eq .Status.State "succeeded"}}A versão {{.Target}} está rodando.{{end}}</p>
{{if .Status.Log}}<ul class="progress" data-update-log>{{range .Status.Log}}<li>{{.}}</li>{{end}}</ul>{{else}}<ul class="progress" data-update-log hidden></ul>{{end}}
{{if not .Finished}}<p class="muted">No meio da atualização o painel reinicia e fica alguns segundos fora do ar. Esta página continua conferindo sozinha e avisa quando a nova versão subir; aí é só entrar de novo.</p>{{end}}
{{else}}<p class="muted">Quando houver uma versão nova, o aviso aparece no topo de todas as páginas do painel, com o botão para atualizar.</p>{{end}}
<p class="muted">Rodando agora: <strong data-update-running>{{.Running}}</strong>{{if eq .Method "dokploy"}} · atualizado pelo Dokploy{{end}}</p>
<div class="actions"><a class="btn btn--ghost" href="/">Voltar ao painel</a></div>
</div></section>
{{template "foot"}}{{end}}
`
