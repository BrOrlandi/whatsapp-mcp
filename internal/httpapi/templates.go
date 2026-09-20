package httpapi

import "strings"

// darkPalette is the panel in the dark theme. The same declarations serve two
// selectors — the system preference and an explicit choice from the masthead —
// so they live here once and are stitched into the stylesheet where the marker
// sits, instead of being kept in sync by hand.
const darkPalette = `
--bg:#0a1513;--surface:#11211d;--surface-soft:#152b26;--surface-sunken:#0d1b18;--border:#23413a;--border-strong:#2f544b;
--text:#e4f1ec;--text-soft:#cbe0d8;--muted:#a7c0b8;
--brand:#2fc9a0;--brand-strong:#4adcb4;--brand-ink:#04211b;--brand-soft:#123029;
--accent:#93b4ff;--accent-soft:#16224a;
--danger:#ffaea7;--danger-bg:#361917;--danger-border:#67312e;
--ok:#6fdcaa;--ok-bg:#0f3526;--ok-border:#1d5a40;
--warn:#f2ce85;--warn-bg:#352a10;--off:#a4b9bf;--off-bg:#1a292d;
--shadow:0 1px 2px rgba(0,0,0,.4),0 8px 24px rgba(0,0,0,.3);
--shadow-lift:0 12px 40px rgba(0,0,0,.5);
`

// darkMarker is where the stylesheet asks for the dark palette.
const darkMarker = "/*dark-palette*/"

// pages is the stylesheet-complete template source the panel parses.
var pages = strings.NewReplacer(darkMarker, darkPalette).Replace(pageSource)

// pageSource holds every template the control panel renders.
//
// The panel is split by task rather than stacked on one screen: connecting a
// client, managing instances and reading the service state are different jobs
// done at different moments, and putting them on one page made the flow hard to
// follow. Dialogs use the :target selector so creating something never needs
// JavaScript to work.
const pageSource = `
{{define "head"}}<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="/favicon.svg" type="image/svg+xml"><link rel="alternate icon" href="/favicon.ico" sizes="16x16 32x32 48x48"><link rel="apple-touch-icon" href="/apple-touch-icon.png"><script src="/assets/theme.js"></script>{{if .Refresh}}<meta http-equiv="refresh" content="5">{{end}}<title>{{.Title}} · WhatsApp MCP</title><style>
*,*::before,*::after{box-sizing:border-box}
:root{
color-scheme:light dark;
--bg:#eef3f1;--surface:#fff;--surface-soft:#f4f9f7;--surface-sunken:#e9f0ed;--border:#dbe6e1;--border-strong:#c3d5cd;
--text:#10241c;--text-soft:#2c443d;--muted:#54695f;
--brand:#0b6b5d;--brand-strong:#0a8172;--brand-ink:#fff;--brand-soft:#e3f3ef;
--accent:#1d4ed8;--accent-soft:#e8eeff;
--danger:#96201f;--danger-bg:#fdeceb;--danger-border:#f0c6c3;
--ok:#0b6b45;--ok-bg:#e1f4ea;--ok-border:#b6e0c9;
--warn:#7a5200;--warn-bg:#fbf0d6;--off:#4f646b;--off-bg:#e8eef0;
--radius:14px;--radius-sm:10px;--ring:#12a08c;
--shadow:0 1px 2px rgba(16,36,28,.06),0 8px 24px rgba(16,36,28,.06);
--shadow-lift:0 12px 40px rgba(16,36,28,.18);
}
@media (prefers-color-scheme:dark){:root:not([data-theme=light]){/*dark-palette*/}}
:root[data-theme=light]{color-scheme:light}
:root[data-theme=dark]{color-scheme:dark;/*dark-palette*/}
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
.masthead__tools{display:flex;align-items:center;gap:8px}
.theme{display:inline-flex}
.theme[hidden]{display:none}
.theme__select{appearance:none;-webkit-appearance:none;font:inherit;font-size:1rem;line-height:1;width:38px;height:38px;padding:0;text-align:center;text-align-last:center;border:1px solid var(--border-strong);border-radius:var(--radius-sm);background:var(--surface);color:var(--text);cursor:pointer}
.theme__select:hover{background:var(--surface-soft)}
.brand{display:flex;align-items:center;gap:12px;min-width:0;text-decoration:none;color:inherit}
.brand__mark{width:38px;height:38px;flex:none}
.brand__mark svg{width:100%;height:100%;display:block}
.brand__name{font-weight:700;font-size:1rem;color:var(--brand);letter-spacing:-.01em}
.brand__tagline{display:block;font-weight:400;font-size:.78rem;color:var(--muted);letter-spacing:0}
/* ---- tools (documentação) ---- */
.tools{display:grid;gap:14px;margin-top:24px}
.tool{border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);box-shadow:var(--shadow);overflow:hidden}
.tool__head{display:flex;flex-wrap:wrap;align-items:baseline;gap:10px;padding:16px 18px 0}
.tool__name{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-weight:700;color:var(--brand);font-size:1rem}
.tool__desc{margin:0;padding:8px 18px 16px;color:var(--text-soft);font-size:.95rem;line-height:1.7;max-width:76ch}
.tool__args{margin:0;padding:14px 18px 16px;list-style:none;display:grid;gap:12px;border-top:1px solid var(--border);background:var(--surface-soft)}
.tool__arg{display:flex;flex-wrap:wrap;gap:8px;align-items:baseline;font-size:.9rem;line-height:1.65}
.tool__argname{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-weight:600;color:var(--text)}
.tool__type{color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.82rem}
.tool__req{font-size:.7rem;text-transform:uppercase;letter-spacing:.06em;font-weight:700;color:var(--warn)}
.tool__argdesc{color:var(--text-soft);flex:1 1 240px;min-width:0}

/* ---- recipes ---- */
.recipes{display:grid;gap:20px;margin-top:24px}
.recipe{border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);box-shadow:var(--shadow);overflow:hidden}
.recipe__head{padding:clamp(18px,3vw,22px) clamp(18px,3vw,24px) 0}
.recipe__title{margin:0 0 10px;font-size:1.18rem;letter-spacing:-.01em}
.recipe__summary{margin:0;color:var(--text-soft);font-size:.97rem;line-height:1.75;max-width:68ch}
.recipe__meta{display:flex;flex-wrap:wrap;gap:8px;padding:16px clamp(18px,3vw,24px) 20px}
.recipe__tool{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.78rem;line-height:1.5;padding:4px 10px;border-radius:999px;background:var(--surface-soft);border:1px solid var(--border-strong);color:var(--text-soft)}
.recipe__tool--when{font-family:inherit;background:var(--brand-soft);border-color:var(--brand-soft);color:var(--brand-strong);font-weight:600}
.recipe__prompt{padding:0 clamp(18px,3vw,24px) clamp(18px,3vw,22px)}
.recipe__prompt .snippet{margin:0}
.recipe__prompt pre{border:1px solid var(--border)}
.recipe__caveat{margin:0;padding:14px clamp(18px,3vw,24px) 16px;border-top:1px solid var(--border);background:var(--surface-soft);font-size:.9rem;color:var(--text-soft);line-height:1.7}
.recipe__caveat strong{color:var(--warn)}
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
.lead{color:var(--text-soft);margin-top:0;font-size:1.02rem;line-height:1.72;max-width:72ch}

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
.btn[disabled]{cursor:progress;opacity:.72}
.btn[disabled]:hover{background:var(--brand)}
.btn[aria-disabled=true]{pointer-events:none;opacity:.45}
.spinner{width:15px;height:15px;flex:none;border:2px solid currentColor;border-right-color:transparent;border-radius:50%;animation:spin .7s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
@media (prefers-reduced-motion:reduce){.spinner{animation-duration:2.4s}}
.busy{display:flex;align-items:center;gap:10px;margin:14px 0 0;color:var(--muted);font-size:.88rem;line-height:1.5}
.busy[hidden]{display:none}

/* ---- numbered how-to list ---- */
.guide{list-style:none;counter-reset:guide;margin:0;padding:0;display:grid;gap:9px}
.guide li{position:relative;counter-increment:guide;padding-left:32px;line-height:1.5}
.guide li::before{content:counter(guide);position:absolute;left:0;top:1px;width:22px;height:22px;border-radius:50%;background:var(--brand-soft);color:var(--brand);font-size:.76rem;font-weight:700;display:inline-flex;align-items:center;justify-content:center}

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
.row__remove{margin-left:auto;flex:none;display:inline-flex;align-items:center;justify-content:center;width:30px;height:30px;border-radius:8px;text-decoration:none;color:var(--muted);font-size:.95rem;line-height:1;border:1px solid transparent}
.row__remove:hover{background:var(--danger-bg);border-color:var(--danger-border);color:var(--danger)}
.target{display:flex;flex-wrap:wrap;align-items:center;gap:8px 10px;padding:12px 14px;margin-bottom:14px;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-soft)}
.target__name{font-weight:700;overflow-wrap:anywhere}
.target__meta{color:var(--muted);font-size:.88rem;overflow-wrap:anywhere}
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
.muted{color:var(--text-soft);font-size:.9rem;line-height:1.65}
.stack>*+*{margin-top:16px}
@media (max-width:520px){.row{align-items:flex-start}.row form,.row .btn{width:100%}}
/* ---- colophon ---- */
.colophon{margin-top:40px;padding:18px 0 4px;border-top:1px solid var(--border)}
.colophon__line{display:flex;flex-wrap:wrap;align-items:center;justify-content:center;gap:8px;margin:0;font-size:.85rem;color:var(--muted)}
.colophon__link{display:inline-flex;align-items:center;gap:5px;color:var(--muted);text-decoration:none;font-weight:600}
.colophon__link:hover{color:var(--brand-strong);text-decoration:underline}
.colophon__icon{flex:none;display:block}
.colophon__sep{color:var(--border-strong)}
.colophon__support{display:inline-flex;align-items:center;gap:6px;padding:4px 12px;border-radius:999px;background:var(--brand-soft);border:1px solid var(--border);color:var(--brand);font-weight:700;text-decoration:none}
.colophon__support:hover{background:var(--brand);color:var(--brand-ink);border-color:var(--brand)}
</style></head><body><div class="shell">{{end}}

{{define "foot"}}
<footer class="colophon">
<p class="colophon__line">
<a class="colophon__link" href="{{authorURL}}" rel="noopener noreferrer" target="_blank">{{author}}</a>
<span class="colophon__sep">&middot;</span>
<a class="colophon__link" href="{{repositoryURL}}" rel="noopener noreferrer" target="_blank">{{template "githubmark"}}<span>C&oacute;digo-fonte</span></a>
{{with supportURL}}<span class="colophon__sep">&middot;</span>
<a class="colophon__support" href="{{.}}" rel="noopener noreferrer" target="_blank">Apoie o projeto</a>{{end}}
</p>
</footer>
<script src="/assets/app.js" defer></script></div></body></html>{{end}}

{{define "githubmark"}}<svg class="colophon__icon" viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" focusable="false"><path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.4 7.4 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"/></svg>{{end}}

{{define "themeswitch"}}<span class="theme" hidden data-theme-switch>
<select class="theme__select" aria-label="Tema da interface" data-theme-select>
<option value="system" title="Seguir o sistema">◐</option>
<option value="light" title="Tema claro">☀</option>
<option value="dark" title="Tema escuro">☾</option>
</select></span>{{end}}

{{define "brandmark"}}<span class="brand__mark">{{logo}}</span><span class="brand__name">WhatsApp MCP<span class="brand__tagline">Painel de controle</span></span>{{end}}

{{define "nav"}}
<header class="masthead"><a class="brand" href="/">{{template "brandmark"}}</a>
<div class="masthead__tools">{{template "themeswitch"}}
<a class="btn btn--quiet" href="/senha">Senha</a>
<form method="post" action="/logout"><button class="btn btn--quiet" type="submit">Sair</button></form></div></header>
<nav class="nav" aria-label="Seções">
<a href="/"{{if eq .Active "conectar"}} aria-current="page"{{end}}>Conectar</a>
<a href="/instancias"{{if eq .Active "instancias"}} aria-current="page"{{end}}>Instâncias</a>
<a href="/estado"{{if eq .Active "estado"}} aria-current="page"{{end}}><span class="nav__dot nav__dot--{{.SessionTone}}"></span>Estado</a>
<a href="/documentacao"{{if eq .Active "documentacao"}} aria-current="page"{{end}}>Documentação</a>
<a href="/receitas"{{if eq .Active "receitas"}} aria-current="page"{{end}}>Receitas</a>
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
{{range $i, $inst := .Instances}}<li class="row{{if .Selected}} row--on{{end}}">
<label class="row__label" for="instance-{{.ID}}">
<input id="instance-{{.ID}}" type="radio" name="instance_id" value="{{.ID}}"{{if .Selected}} checked{{end}}>
<span class="row__main"><span class="row__title">{{.Name}}</span>{{if .Number}}<span class="row__meta mono">{{.Number}}</span>{{end}}</span>
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
</div>
<p class="muted">Para apagar uma instância, use o ✕ na linha dela, na lista acima.</p>
</div>
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
{{if .Number}}<span class="target__meta mono">{{.Number}}</span>{{else}}<span class="target__meta">Sem número: ainda não pareada.</span>{{end}}
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
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha<span class="field__hint">Mínimo de 10 caracteres.</span></span></label>
<input id="password" type="password" name="password" minlength="10" required autocomplete="new-password">
<div class="actions" style="margin-top:16px"><button class="btn btn--block" type="submit">Criar administrador</button></div>
</form>{{end}}
</div></section>
<p class="muted" style="text-align:center">As senhas são guardadas com bcrypt e nunca aparecem nos logs.</p>
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
<label class="field" for="password"><span class="field__label">Nova senha<span class="field__hint">M&iacute;nimo de 10 caracteres.</span></span></label>
<input id="password" type="password" name="password" minlength="10" required autocomplete="new-password">
<label class="field" for="confirm"><span class="field__label">Repita a nova senha</span></label>
<input id="confirm" type="password" name="confirm_password" minlength="10" required autocomplete="new-password">
<div class="actions" style="margin-top:16px"><button class="btn btn--block" type="submit">Salvar senha</button></div>
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
<label class="field" for="username"><span class="field__label">Usuário</span></label>
<input id="username" type="text" name="username" required autocomplete="username" autocapitalize="none" spellcheck="false">
<label class="field" for="password"><span class="field__label">Senha</span></label>
<input id="password" type="password" name="password" required autocomplete="current-password">
<div class="actions" style="margin-top:16px"><button class="btn btn--block" type="submit">Entrar</button></div>
</form>
</div></section>
</div>
{{template "foot"}}{{end}}
{{define "documentacao"}}{{template "head" .}}{{template "nav" .}}
<h1>O que o MCP sabe fazer</h1>
<p class="lead">{{.Count}} ferramentas, lidas do próprio servidor. Esta página não é uma cópia mantida à mão: ela descreve exatamente a superfície que o MCP publica, então só fica errada se o servidor estiver.</p>
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
</div>
{{template "foot"}}{{end}}

{{define "receitas"}}{{template "head" .}}{{template "nav" .}}
<h1>Receitas</h1>
<p class="lead">Nenhuma destas precisa de código novo. O gateway só responde pelo WhatsApp quando perguntado — esperar a hora, vigiar um termo, montar o relatório, tudo isso é trabalho do assistente, escrito como instrução. Cada receita é um prompt para colar.</p>
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
</div>
{{template "foot"}}{{end}}`
