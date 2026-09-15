package httpapi

// pages holds every template the control panel renders.
//
// The panel is split by task rather than stacked on one screen: connecting a
// client, managing instances and reading the service state are different jobs
// done at different moments, and putting them on one page made the flow hard to
// follow. Dialogs use the :target selector so creating something never needs
// JavaScript to work.
const pages = `
{{define "head"}}<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Refresh}}<meta http-equiv="refresh" content="5">{{end}}<title>{{.Title}} · WhatsApp MCP</title><style>
*,*::before,*::after{box-sizing:border-box}
:root{
color-scheme:light dark;
--bg:#eef3f1;--surface:#fff;--surface-soft:#f4f9f7;--surface-sunken:#e9f0ed;--border:#dbe6e1;--border-strong:#c3d5cd;
--text:#10241c;--muted:#5d726b;
--brand:#0b6b5d;--brand-strong:#0a8172;--brand-ink:#fff;--brand-soft:#e3f3ef;
--accent:#1d4ed8;--accent-soft:#e8eeff;
--danger:#96201f;--danger-bg:#fdeceb;--danger-border:#f0c6c3;
--ok:#0b6b45;--ok-bg:#e1f4ea;--ok-border:#b6e0c9;
--warn:#7a5200;--warn-bg:#fbf0d6;--off:#4f646b;--off-bg:#e8eef0;
--radius:14px;--radius-sm:10px;--ring:#12a08c;
--shadow:0 1px 2px rgba(16,36,28,.06),0 8px 24px rgba(16,36,28,.06);
--shadow-lift:0 12px 40px rgba(16,36,28,.18);
}
@media (prefers-color-scheme:dark){:root{
--bg:#0a1513;--surface:#11211d;--surface-soft:#152b26;--surface-sunken:#0d1b18;--border:#23413a;--border-strong:#2f544b;
--text:#e4f1ec;--muted:#93aca4;
--brand:#2fc9a0;--brand-strong:#4adcb4;--brand-ink:#04211b;--brand-soft:#123029;
--accent:#93b4ff;--accent-soft:#16224a;
--danger:#ffaea7;--danger-bg:#361917;--danger-border:#67312e;
--ok:#6fdcaa;--ok-bg:#0f3526;--ok-border:#1d5a40;
--warn:#f2ce85;--warn-bg:#352a10;--off:#a4b9bf;--off-bg:#1a292d;
--shadow:0 1px 2px rgba(0,0,0,.4),0 8px 24px rgba(0,0,0,.3);
--shadow-lift:0 12px 40px rgba(0,0,0,.5);
}}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
h1,h2,h3{line-height:1.25;margin:0;color:var(--text)}
h1{font-size:clamp(1.35rem,1.1rem + 1.2vw,1.7rem)}
h2{font-size:1.15rem}
h3{font-size:.95rem;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}
p{margin:.6em 0}
a{color:var(--brand-strong)}
code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.88em;background:var(--surface-sunken);border:1px solid var(--border);padding:1px 5px;border-radius:5px;overflow-wrap:anywhere}
pre{position:relative;overflow:auto;background:#07201b;color:#dffff4;padding:14px 16px;border-radius:var(--radius-sm);font-size:.85rem;line-height:1.55;margin:0}
pre code{background:none;border:0;padding:0;color:inherit;font-size:1em}

/* ---- shell ---- */
.shell{max-width:1000px;margin:0 auto;padding:0 clamp(16px,4vw,24px) 72px}
.masthead{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;padding:20px 0 14px}
.brand{display:flex;align-items:center;gap:12px;min-width:0;text-decoration:none;color:inherit}
.brand__mark{width:38px;height:38px;flex:none}
.brand__mark svg{width:100%;height:100%;display:block}
.brand__name{font-weight:700;font-size:1rem;color:var(--brand);letter-spacing:-.01em}
.brand__tagline{display:block;font-weight:400;font-size:.78rem;color:var(--muted);letter-spacing:0}
.nav{display:flex;flex-wrap:wrap;gap:4px;border-bottom:1px solid var(--border);margin-bottom:24px}
.nav a{position:relative;display:inline-flex;align-items:center;gap:7px;padding:10px 14px;text-decoration:none;color:var(--muted);font-weight:600;font-size:.94rem;border-radius:var(--radius-sm) var(--radius-sm) 0 0;border-bottom:2px solid transparent;margin-bottom:-1px}
.nav a:hover{color:var(--text);background:var(--surface-soft)}
.nav a[aria-current=page]{color:var(--brand);border-bottom-color:var(--brand)}
.nav__dot{width:8px;height:8px;border-radius:50%;background:currentColor;flex:none}
.nav__dot--ok{background:var(--ok)}
.nav__dot--off{background:var(--off)}
.nav__dot--warn{background:var(--warn)}
.nav__spacer{flex:1}

/* ---- cards ---- */
.card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);box-shadow:var(--shadow);margin:0 0 18px;overflow:hidden}
.card__head{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:10px 16px;padding:16px clamp(16px,3vw,22px);border-bottom:1px solid var(--border);background:var(--surface-soft)}
.card__body{padding:clamp(16px,3vw,22px)}
.card__body>:first-child{margin-top:0}
.card__body>:last-child{margin-child:0}
.card--accent .card__head{background:var(--brand-soft);border-bottom-color:var(--border-strong)}
.card--accent .card__head h2{color:var(--brand)}
.lead{color:var(--muted);margin-top:0}

/* ---- steps ---- */
.step{padding:4px 0}
.step+.step{border-top:1px solid var(--border)}
.step__summary{display:flex;align-items:center;gap:12px;padding:14px 0;cursor:pointer;list-style:none}
.step__summary::-webkit-details-marker{display:none}
.step__summary:hover .step__title{color:var(--brand)}
.step[data-locked] .step__summary{cursor:default;opacity:.6}
.step__n{width:30px;height:30px;flex:none;border-radius:50%;background:var(--brand);color:var(--brand-ink);display:grid;place-items:center;font-weight:700;font-size:.9rem}
.step--done .step__n{background:var(--ok);font-size:1rem}
.step[data-locked] .step__n{background:var(--off-bg);color:var(--off)}
.step__title{font-weight:700}
.step--done .step__title{color:var(--muted);font-weight:600}
.step__state{font-size:.72rem;font-weight:700;text-transform:uppercase;letter-spacing:.06em;color:var(--ok)}
.step__state--waiting{color:var(--warn);display:inline-flex;align-items:center;gap:6px}
.step__state--waiting::before{content:"";width:7px;height:7px;border-radius:50%;background:currentColor;animation:pulse 1.6s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.25}}
@media (prefers-reduced-motion:reduce){.step__state--waiting::before{animation:none}}
.step__body{padding:0 0 18px 42px}
.step__body>:first-child{margin-top:0}
@media (max-width:520px){.step__body{padding-left:0}}

/* ---- pills, badges ---- */
.pill{display:inline-flex;align-items:center;gap:6px;font-size:.8rem;font-weight:600;padding:4px 10px;border-radius:999px;white-space:nowrap;border:1px solid transparent}
.pill::before{content:"";width:7px;height:7px;border-radius:50%;background:currentColor;flex:none}
.pill--ok{background:var(--ok-bg);color:var(--ok);border-color:var(--ok-border)}
.pill--warn{background:var(--warn-bg);color:var(--warn)}
.pill--off{background:var(--off-bg);color:var(--off)}
.pill--plain::before{display:none}
.pill--accent{background:var(--accent-soft);color:var(--accent)}

/* ---- buttons ---- */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;font:inherit;font-size:.94rem;font-weight:600;text-decoration:none;padding:10px 16px;border:1px solid transparent;border-radius:var(--radius-sm);cursor:pointer;background:var(--brand);color:var(--brand-ink);white-space:nowrap}
.btn:hover{background:var(--brand-strong)}
.btn--block{width:100%}
.btn--ghost{background:var(--surface);color:var(--brand-strong);border-color:var(--border-strong)}
.btn--ghost:hover{background:var(--surface-soft)}
.btn--quiet{background:transparent;color:var(--muted);border-color:transparent;padding:8px 10px;font-weight:500}
.btn--quiet:hover{background:var(--surface-soft);color:var(--text)}
.btn--danger{background:transparent;color:var(--danger);border-color:var(--danger-border)}
.btn--danger:hover{background:var(--danger-bg)}
.btn--small{padding:6px 11px;font-size:.86rem}
:where(a,button,input,summary):focus-visible{outline:3px solid var(--ring);outline-offset:2px}
.actions{display:flex;flex-wrap:wrap;gap:10px;align-items:center}
.actions--end{justify-content:flex-end}

/* ---- forms ---- */
.field{display:block;margin:0 0 14px}
.field__label{display:block;font-weight:600;font-size:.9rem;margin-bottom:5px}
.field__hint{display:block;font-weight:400;color:var(--muted);font-size:.84rem;margin-top:2px}
input[type=text],input[type=password]{width:100%;font:inherit;padding:10px 12px;color:var(--text);background:var(--surface-soft);border:1px solid var(--border-strong);border-radius:var(--radius-sm)}
input[type=text]:hover,input[type=password]:hover{border-color:var(--brand-strong)}
input[type=radio]{accent-color:var(--brand-strong);width:18px;height:18px;flex:none;margin:0}

/* ---- alerts ---- */
.alert{display:block;margin:0 0 18px;padding:12px 14px;border-radius:var(--radius-sm);border:1px solid var(--danger-border);background:var(--danger-bg);color:var(--danger);font-size:.93rem}
.alert--ok{border-color:var(--ok-border);background:var(--ok-bg);color:var(--ok)}
.problems{list-style:none;margin:0 0 16px;padding:0;display:grid;gap:8px}
.problems li{padding:11px 13px;border-radius:var(--radius-sm);border:1px solid var(--danger-border);background:var(--danger-bg);color:var(--danger);font-size:.93rem}

/* ---- lists ---- */
.rows{list-style:none;margin:0;padding:0;border:1px solid var(--border);border-radius:var(--radius-sm);overflow:hidden}
.row{display:flex;flex-wrap:wrap;align-items:center;gap:10px 14px;padding:13px 15px;background:var(--surface)}
.row+.row{border-top:1px solid var(--border)}
.row--on{background:var(--brand-soft)}
.row__main{flex:1 1 220px;min-width:0}
.row__title{font-weight:600;overflow-wrap:anywhere}
.row__meta{display:block;font-weight:400;font-size:.84rem;color:var(--muted)}
.row__label{display:flex;align-items:center;gap:11px;flex:1 1 220px;min-width:0;cursor:pointer}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}

/* ---- facts ---- */
.facts{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:14px;margin:0}
.fact{padding:12px 14px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-soft)}
.fact dt{font-size:.78rem;color:var(--muted);font-weight:600;text-transform:uppercase;letter-spacing:.04em}
.fact dd{margin:3px 0 0;font-weight:600;font-size:1.02rem}
.fact__detail{display:block;font-weight:400;font-size:.82rem;color:var(--muted)}

/* ---- empty ---- */
.empty{padding:28px 20px;text-align:center;border:1px dashed var(--border-strong);border-radius:var(--radius-sm);background:var(--surface-soft)}
.empty__title{font-weight:600;color:var(--text);margin:0}

/* ---- snippets ---- */
.snippet{margin:0 0 16px}
.snippet__head{display:flex;flex-wrap:wrap;align-items:baseline;justify-content:space-between;gap:8px;margin-bottom:7px}
.snippet__title{font-weight:600;font-size:.92rem}
.snippet__note{font-size:.84rem;color:var(--muted)}
.copy{font:inherit;font-size:.78rem;font-weight:600;padding:4px 10px;border-radius:7px;border:1px solid var(--border-strong);background:var(--surface);color:var(--brand-strong);cursor:pointer;flex:none}
.snippet--loose{position:relative}
.snippet--loose .copy{position:absolute;top:8px;right:8px;z-index:1}
.copy:hover{background:var(--surface-soft)}
.copy--done{color:var(--ok);border-color:var(--ok-border)}
.copy--failed{color:var(--danger);border-color:var(--danger-border)}

/* ---- secret ---- */
.secret{border:2px solid var(--ok);border-radius:var(--radius);background:var(--ok-bg);padding:18px;margin:0 0 20px}
.secret__title{margin:0 0 4px;font-weight:700;color:var(--ok)}
.secret__value{display:block;margin:12px 0 8px;padding:14px;border-radius:var(--radius-sm);background:var(--surface);border:1px solid var(--ok-border);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:1.05rem;font-weight:600;overflow-wrap:anywhere;user-select:all}

/* ---- dialogs (CSS only) ---- */
.overlay{position:fixed;inset:0;background:rgba(6,20,16,.55);display:none;place-items:center;padding:20px;z-index:50}
.overlay:target{display:grid}
.dialog{background:var(--surface);border:1px solid var(--border-strong);border-radius:var(--radius);box-shadow:var(--shadow-lift);width:min(460px,100%);max-height:90vh;overflow:auto}
.dialog__head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:16px 20px;border-bottom:1px solid var(--border)}
.dialog__head h2{font-size:1.05rem}
.dialog__body{padding:20px}
.dialog__close{text-decoration:none;color:var(--muted);font-size:1.5rem;line-height:1;padding:0 4px}
.dialog__close:hover{color:var(--text)}

.tabs__radio{position:absolute;width:1px;height:1px;opacity:0;pointer-events:none}
.tabs__bar{display:flex;gap:4px;border-bottom:1px solid var(--border);margin-bottom:16px}
.tabs__tab{padding:8px 14px;font-weight:600;font-size:.92rem;color:var(--muted);cursor:pointer;border-bottom:2px solid transparent;margin-bottom:-1px;border-radius:var(--radius-sm) var(--radius-sm) 0 0}
.tabs__tab:hover{color:var(--text);background:var(--surface-soft)}
.tabs__panel{display:none}
#tab-code:checked~.tabs__panel--code,#tab-desktop:checked~.tabs__panel--desktop{display:block}
#tab-code:checked~.tabs__bar label[for=tab-code],#tab-desktop:checked~.tabs__bar label[for=tab-desktop]{color:var(--brand);border-bottom-color:var(--brand)}
.tabs__radio:focus-visible~.tabs__bar label{outline:3px solid var(--ring);outline-offset:2px}
.prompts{list-style:none;margin:12px 0 0;padding:0;display:grid;gap:8px}
.prompt .snippet{margin:0}
.prompt pre{background:var(--surface-sunken);color:var(--text);border:1px solid var(--border);padding:10px 12px;font-family:inherit;font-size:.92rem;white-space:pre-wrap}
.qrcode{display:block;margin:0 auto;width:250px;height:250px;max-width:100%;background:#fff;padding:12px;border-radius:var(--radius-sm);border:1px solid var(--border)}
.sr-only{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
.muted{color:var(--muted);font-size:.9rem}
.stack>*+*{margin-top:16px}
@media (max-width:520px){.row{align-items:flex-start}.row form,.row .btn{width:100%}}
</style></head><body><div class="shell">{{end}}

{{define "foot"}}<script src="/assets/app.js" defer></script></div></body></html>{{end}}

{{define "brandmark"}}<span class="brand__mark">{{logo}}</span><span class="brand__name">WhatsApp MCP<span class="brand__tagline">Painel de controle</span></span>{{end}}

{{define "nav"}}
<header class="masthead"><a class="brand" href="/">{{template "brandmark"}}</a>
<form method="post" action="/logout"><button class="btn btn--quiet" type="submit">Sair</button></form></header>
<nav class="nav" aria-label="Seções">
<a href="/"{{if eq .Active "conectar"}} aria-current="page"{{end}}>Conectar</a>
<a href="/instancias"{{if eq .Active "instancias"}} aria-current="page"{{end}}>Instâncias</a>
<a href="/estado"{{if eq .Active "estado"}} aria-current="page"{{end}}><span class="nav__dot nav__dot--{{.SessionTone}}"></span>Estado</a>
</nav>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
{{end}}

{{define "clientTabs"}}
<div class="tabs">
<input class="tabs__radio" type="radio" name="cliente" id="tab-code" checked>
<input class="tabs__radio" type="radio" name="cliente" id="tab-desktop">
<div class="tabs__bar" role="tablist">
<label class="tabs__tab" for="tab-code">Claude Code</label>
<label class="tabs__tab" for="tab-desktop">Claude Desktop</label>
</div>

<div class="tabs__panel tabs__panel--code">
<p class="muted">Um comando no terminal, de dentro de qualquer projeto.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Comando</span></div>
<pre data-copy><code>{{.Command}}</code></pre></div>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Sem a chave no arquivo</span><span class="snippet__note">opcional</span></div>
<pre data-copy><code>{{.CommandEnv}}</code></pre></div>
<p class="muted">Na segunda forma o Claude Code lê a chave de <code>WHATSAPP_MCP_KEY</code>, que você exporta no seu shell, e ela não fica na configuração. Confira depois com <code>claude mcp list</code>.</p>
</div>

<div class="tabs__panel tabs__panel--desktop">
<p class="muted">Em <strong>Configurações → Desenvolvedor → Editar configuração</strong>, cole o bloco abaixo e reinicie o aplicativo.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">claude_desktop_config.json</span></div>
<pre data-copy><code>{{.JSON}}</code></pre></div>
<p class="muted">Se o arquivo já tiver outros servidores, acrescente só o trecho <code>"whatsapp"</code> dentro de <code>mcpServers</code>.</p>
</div>
</div>
{{if not .HasSecret}}<p class="muted">Troque <code>SUA_CHAVE</code> pela chave que você guardou. Se não guardou, gere outra — a chave é exibida uma única vez.</p>{{end}}
{{end}}

{{define "conectar"}}{{template "head" .}}{{template "nav" .}}
<h1>Conectar um cliente a este MCP</h1>
<p class="lead">O cliente precisa de uma única informação: a chave de API. Ela já identifica a conta e a instância do WhatsApp.</p>

{{if .Ready}}
<section class="card card--accent">
<div class="card__head"><h2>Passo a passo</h2><span class="pill pill--ok">Instância {{.InstanceName}} conectada</span></div>
<div class="card__body">

<details class="step{{if .HasKey}} step--done{{end}}"{{if not .HasKey}} open{{end}}>
<summary class="step__summary">
<span class="step__n" aria-hidden="true">{{if .HasKey}}✓{{else}}1{{end}}</span>
<span class="step__title">Gere uma chave</span>
{{if .HasKey}}<span class="step__state">concluído</span>{{end}}
</summary>
<div class="step__body">
{{if .HasKey}}
<p class="muted">{{plural (len64 .Keys) "chave ativa" "chaves ativas"}}. Uma por cliente, para revogar um sem derrubar os outros.</p>
<div class="actions"><a class="btn btn--ghost btn--small" href="#nova-chave">Gerar outra chave</a></div>
{{else}}
<p class="muted">Uma chave por cliente. Assim dá para revogar um sem derrubar os outros.</p>
<div class="actions"><a class="btn" href="#nova-chave">Gerar nova chave</a></div>
{{end}}
</div></details>

<details class="step{{if .ClientConnected}} step--done{{end}}"{{if and .HasKey (not .ClientConnected)}} open{{end}}{{if not .HasKey}} data-locked="true"{{end}}>
<summary class="step__summary">
<span class="step__n" aria-hidden="true">{{if .ClientConnected}}✓{{else}}2{{end}}</span>
<span class="step__title">Configure o cliente</span>
{{if .ClientConnected}}<span class="step__state">concluído</span>{{else if .HasKey}}<span class="step__state step__state--waiting" data-progress data-connected="false">aguardando conexão</span>{{end}}
</summary>
<div class="step__body">
{{if .ClientConnected}}<p class="muted">Um cliente se autenticou {{relativeSince .LastUse}}. Nada mais a fazer aqui.</p>{{end}}
{{if .HasKey}}
<p class="muted">Endpoint deste MCP: <code>{{.Endpoint}}</code> — fixo e não é segredo. A autenticação é o <code>Authorization: Bearer</code> com a sua chave.</p>
{{template "clientTabs" .Setup}}
{{if not .ClientConnected}}<p class="muted">Assim que o cliente fizer a primeira chamada, este passo se marca sozinho — esta página detecta e atualiza.</p>{{end}}
{{else}}
<p class="muted">Gere a chave primeiro. Ela vem com o comando e o JSON já preenchidos.</p>
{{end}}
</div></details>

<details class="step{{if .ClientConnected}} step--done{{end}}"{{if .ClientConnected}} open{{end}}{{if not .ClientConnected}} data-locked="true"{{end}}>
<summary class="step__summary">
<span class="step__n" aria-hidden="true">{{if .ClientConnected}}✓{{else}}3{{end}}</span>
<span class="step__title">Use</span>
{{if .ClientConnected}}<span class="step__state">pronto</span>{{end}}
</summary>
<div class="step__body">
<p class="muted">As ferramentas aparecem sozinhas depois que o cliente reinicia. Experimente pedir:</p>
<ul class="prompts">
{{range .Prompts}}<li class="prompt"><div class="snippet"><pre data-copy><code>{{.}}</code></pre></div></li>{{end}}
</ul>
<p class="muted">Leitura e busca são seguras. Envio só acontece quando você pede explicitamente.</p>
</div></details>
</div></section>

<section class="card">
<div class="card__head"><h2>Chaves ativas</h2><a class="btn btn--ghost btn--small" href="#nova-chave">Nova chave</a></div>
<div class="card__body">
{{if .Keys}}
<ul class="rows">
{{range .Keys}}<li class="row">
<span class="row__main"><span class="row__title">{{.Name}}</span><span class="row__meta mono">{{.Prefix}}…</span></span>
<span class="muted">criada em {{moment .CreatedAt}} · último uso {{relativeSince .LastUsedAt}}</span>
<form method="post" action="/chaves/revogar"><input type="hidden" name="id" value="{{.ID}}"><button class="btn btn--danger btn--small" type="submit">Revogar</button></form>
</li>{{end}}
</ul>
<p class="muted">Revogar tem efeito imediato: cada requisição do MCP se autentica por conta própria.</p>
{{else}}
<div class="empty"><p class="empty__title">Nenhuma chave ativa</p><p class="muted">Gere a primeira para conectar um cliente.</p><div class="actions" style="justify-content:center;margin-top:14px"><a class="btn" href="#nova-chave">Gerar chave</a></div></div>
{{end}}
</div></section>

{{else}}
<section class="card">
<div class="card__head"><h2>Conecte o WhatsApp primeiro</h2>{{with .SessionLabel}}<span class="pill pill--{{$.SessionTone}}">{{.}}</span>{{end}}</div>
<div class="card__body">
<div class="empty"><p class="empty__title">Ainda não dá para gerar uma chave</p><p class="muted">{{.Notice}}</p>
<div class="actions" style="justify-content:center;margin-top:14px">
{{if .NeedsPairing}}<a class="btn" href="/pair">Ler o QR code</a>{{end}}
<a class="btn btn--ghost" href="/instancias">Ir para Instâncias</a>
</div></div>
</div></section>
{{end}}

<div class="overlay" id="nova-chave" role="dialog" aria-modal="true" aria-labelledby="nova-chave-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="nova-chave-titulo">Gerar uma chave</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<form method="post" action="/chaves">
<label class="field" for="key-name"><span class="field__label">Onde esta chave vai ser usada?</span>
<span class="field__hint">Só um rótulo para você reconhecer depois. Ex.: “Claude Code no notebook”.</span></label>
<input id="key-name" type="text" name="name" maxlength="60" required placeholder="Claude Code no notebook" autocapitalize="sentences" spellcheck="false">
<p class="muted">A chave é gerada agora e exibida uma única vez, junto com a configuração pronta.</p>
<div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn" type="submit">Gerar chave</button></div>
</form>
</div></div></div>
{{template "foot"}}{{end}}

{{define "chave"}}{{template "head" .}}{{template "nav" .}}
<h1>Chave criada</h1>
<p class="lead">Copie agora: esta é a única vez que a chave aparece.</p>

<div class="secret">
<p class="secret__title">{{.Name}}</p>
<code class="secret__value">{{.Secret}}</code>
<p class="muted">Guarde no gerenciador de credenciais do cliente. Nunca em um prompt compartilhável nem em arquivo versionado.</p>
</div>

<section class="card card--accent">
<div class="card__head"><h2>Configure o cliente</h2></div>
<div class="card__body">
{{template "clientTabs" .Setup}}
</div></section>

<section class="card">
<div class="card__head"><h2>Primeiro teste</h2></div>
<div class="card__body">
<p class="muted">Cole no chat depois de reiniciar o cliente, para confirmar que tudo respondeu.</p>
<div class="snippet"><div class="snippet__head"><span class="snippet__title">Prompt de verificação</span></div>
<pre data-copy><code>{{.Prompt}}</code></pre></div>
</div></section>

<div class="actions"><a class="btn btn--ghost" href="/">Voltar para Conectar</a></div>
{{template "foot"}}{{end}}

{{define "instancias"}}{{template "head" .}}{{template "nav" .}}
<h1>Instâncias</h1>
<p class="lead">Uma instância é uma conta de WhatsApp conectada. O MCP usa uma por vez.</p>

<section class="card">
<div class="card__head"><h2>Instâncias disponíveis</h2><a class="btn btn--ghost btn--small" href="#nova-instancia">Adicionar instância</a></div>
<div class="card__body">
{{if .Unavailable}}<p class="alert" role="alert">{{.Notice}}</p>
{{else if .Instances}}
<form method="post" action="/instancias/selecionar">
<ul class="rows">
{{range .Instances}}<li class="row{{if .Selected}} row--on{{end}}">
<label class="row__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if .Selected}} checked{{end}}>
<span class="row__main"><span class="row__title">{{.Name}}</span>{{if .Number}}<span class="row__meta mono">{{.Number}}</span>{{end}}</span>
</label>
<span class="pill pill--{{statusTone .Status}}">{{statusLabel .Status}}</span>
{{if .Selected}}<span class="pill pill--ok pill--plain">Em uso pelo MCP</span>{{end}}
{{if not .Managed}}<span class="pill pill--off pill--plain">Sem credenciais aqui</span>{{end}}
</li>{{end}}
</ul>
<div class="actions" style="margin-top:14px"><button class="btn" type="submit">Usar a selecionada</button></div>
</form>
{{else}}
<div class="empty"><p class="empty__title">Nenhuma instância ainda</p><p class="muted">{{.Notice}}</p>
<div class="actions" style="justify-content:center;margin-top:14px"><a class="btn" href="#nova-instancia">Adicionar instância</a></div></div>
{{end}}
</div></section>

{{if .Selected}}
<section class="card">
<div class="card__head"><h2>Instância em uso: {{.SelectedName}}</h2>{{with .SessionLabel}}<span class="pill pill--{{$.SessionTone}}">{{.}}</span>{{end}}</div>
<div class="card__body stack">
{{if .NeedsPairing}}<p>A instância ainda não está pareada. Leia o QR code para conectar o WhatsApp.</p>
<div class="actions"><a class="btn" href="/pair">Ler o QR code</a></div>{{end}}

<div>
<h3>Conexão</h3>
<div class="actions" style="margin-top:10px">
<form method="post" action="/instancias/conectar"><button class="btn btn--ghost btn--small" type="submit">Reconectar</button></form>
<form method="post" action="/instancias/desconectar"><button class="btn btn--ghost btn--small" type="submit">Desconectar</button></form>
</div>
<p class="muted">Desconectar apenas para o cliente e preserva o pareamento.</p>
</div>

<div>
<h3>Histórico</h3>
<p class="muted">O índice cobre o que chegou desde que a instância foi conectada. O WhatsApp devolve mensagens anteriores a uma que ele já conhece, então cada pedido recua mais um trecho.</p>
<div class="actions"><form method="post" action="/instancias/historico"><button class="btn btn--ghost btn--small" type="submit">Puxar mensagens mais antigas</button></form></div>
</div>

<div>
<h3>Zona de risco</h3>
<div class="actions" style="margin-top:10px">
<a class="btn btn--danger btn--small" href="#encerrar-sessao">Encerrar sessão do WhatsApp</a>
<a class="btn btn--danger btn--small" href="#remover-instancia">Remover instância</a>
</div>
</div>
</div></section>
{{end}}

<div class="overlay" id="nova-instancia" role="dialog" aria-modal="true" aria-labelledby="nova-instancia-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="nova-instancia-titulo">Adicionar instância</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<form method="post" action="/instancias">
<label class="field" for="new-instance"><span class="field__label">Nome da instância</span>
<span class="field__hint">Só para você identificar a conta. Ex.: “pessoal”, “trabalho”.</span></label>
<input id="new-instance" type="text" name="name" maxlength="60" required placeholder="pessoal" autocapitalize="none" spellcheck="false">
<p class="muted">Depois de criar, o painel abre o QR code para você parear o WhatsApp.</p>
<div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn" type="submit">Criar e parear</button></div>
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

<div class="overlay" id="remover-instancia" role="dialog" aria-modal="true" aria-labelledby="remover-titulo">
<div class="dialog">
<div class="dialog__head"><h2 id="remover-titulo">Remover a instância?</h2><a class="dialog__close" href="#" aria-label="Fechar">×</a></div>
<div class="dialog__body">
<p>A instância é apagada e o WhatsApp é desconectado. As chaves de API emitidas para ela param de funcionar.</p>
<p class="muted">As mensagens já indexadas continuam no banco.</p>
<form method="post" action="/instancias/remover"><div class="actions actions--end"><a class="btn btn--quiet" href="#">Cancelar</a><button class="btn btn--danger" type="submit">Remover instância</button></div></form>
</div></div></div>
{{template "foot"}}{{end}}

{{define "estado"}}{{template "head" .}}{{template "nav" .}}
<h1>Estado do serviço</h1>
<p class="lead">O mesmo retrato que as ferramentas do MCP e os endpoints de saúde reportam.</p>

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

{{define "pair"}}{{template "head" .}}{{template "nav" .}}
<h1>Conectar o WhatsApp</h1>
{{with .Name}}<p class="lead">Instância <strong>{{.}}</strong>.</p>{{end}}

<section class="card">
<div class="card__head"><h2>QR code</h2><span class="pill pill--warn">Aguardando leitura</span></div>
<div class="card__body stack">
{{if .QRCode}}
<p>No celular: <strong>WhatsApp → Dispositivos conectados → Conectar um dispositivo</strong>, e aponte a câmera para o código.</p>
<img class="qrcode" src="{{.QRCode}}" alt="QR code para conectar o WhatsApp" width="250" height="250">
{{else}}
<div class="empty"><p class="empty__title">Aguardando o QR code</p><p class="muted">{{.Notice}}</p></div>
{{end}}
{{with .Code}}<p class="muted">Código: <code>{{.}}</code></p>{{end}}
<p class="muted">Esta página se atualiza sozinha a cada 5 segundos. O código expira rápido; se sumir, gere outro.</p>
<div class="actions">
<form method="post" action="/instancias/conectar"><button class="btn btn--ghost" type="submit">Gerar outro código</button></form>
<a class="btn btn--quiet" href="/instancias">Voltar</a>
</div>
</div></section>
{{template "foot"}}{{end}}

{{define "setup"}}{{template "head" .}}
<div style="max-width:460px;margin:0 auto;padding-top:8vh">
<div class="masthead" style="justify-content:center">{{template "brandmark"}}</div>
<section class="card">
<div class="card__head"><h2>Configuração inicial</h2></div>
<div class="card__body">
<p class="lead">Crie o único administrador deste painel. Depois disso esta página deixa de aceitar cadastros.</p>
{{with .Error}}<p class="alert" role="alert">{{.}}</p>{{end}}
<form method="post">
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha<span class="field__hint">Mínimo de 10 caracteres.</span></span></label>
<input id="password" type="password" name="password" minlength="10" required autocomplete="new-password">
<div class="actions" style="margin-top:16px"><button class="btn btn--block" type="submit">Criar administrador</button></div>
</form>
</div></section>
<p class="muted" style="text-align:center">As senhas são guardadas com bcrypt e nunca aparecem nos logs.</p>
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
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha</span></label>
<input id="password" type="password" name="password" required autocomplete="current-password">
<div class="actions" style="margin-top:16px"><button class="btn btn--block" type="submit">Entrar</button></div>
</form>
</div></section>
</div>
{{template "foot"}}{{end}}`
