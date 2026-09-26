<p align="center">
  <img src="internal/brand/logo.svg" alt="" width="88" height="88">
</p>

<h1 align="center">WhatsApp MCP</h1>

<p align="center">
  <strong>Connect your WhatsApp to your AI agents over MCP.</strong><br>
  A self-hosted Go gateway that puts a WhatsApp account behind one authenticated MCP endpoint.
</p>

<p align="center">
  <a href="README.md">🇧🇷 Leia em português</a>
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-0b6b5d"></a>
  <img alt="Go" src="https://img.shields.io/badge/go-1.24-00ADD8">
  <img alt="Self-hosted" src="https://img.shields.io/badge/deploy-docker%20compose-2496ED">
</p>

---

> ### Use this at your own risk
>
> WhatsApp publishes no official API for a personal account. To make an MCP
> server possible at all, this project drives WhatsApp through
> [Evolution Go](https://github.com/EvolutionAPI/evolution-go), an **unofficial**
> client built on [whatsmeow](https://github.com/tulir/whatsmeow) — the same
> mechanism as WhatsApp Web, not the WhatsApp Business API.
>
> WhatsApp does not sanction this. A linked account can be logged out at any
> time, broken by a protocol change, or restricted or banned. Nothing here is
> guaranteed, warranted or supported, and you carry whatever happens to your
> number. Start with an account you can afford to lose.

Ask Claude to read a conversation, search a year of messages, send a file, run a
poll — against your own WhatsApp, on your own server.

> The control panel is in Portuguese. This page is the English translation of
> the [Portuguese README](README.md); panel page names are kept as they appear
> on screen.

<p align="center">
  <img src="docs/assets/panel-conectar.png" alt="The Conectar page of the control panel" width="820">
</p>

## How to install

Three steps, and you need to know neither Docker, nor TLS, nor the command line.

### 1. Rent a server

This runs on a machine of yours; there is no hosted version — the whole point is
that your messages sit on a computer you own. You rent a virtual machine (a VPS)
from a cloud provider, for roughly **US$ 12–25 a month**.

Ask for a machine like this:

| | |
|---|---|
| System | **Ubuntu 24.04 LTS** |
| CPU | 2 vCPU |
| Memory | 4 GB RAM |
| Disk | 80 GB SSD |
| Ports | 80 and 443 open |

**Rent it close to home.** Every message your account sends or receives ends up
on that disk in plain text. Put the machine in the jurisdiction you already
answer to, and close to the people you talk to.

| Provider | Notes |
|---|---|
| [Hetzner](https://www.hetzner.com/cloud) | Best price per GB of RAM; Germany, Finland, US |
| [DigitalOcean](https://www.digitalocean.com/) | Simple panel, many regions |
| [Vultr](https://www.vultr.com/) | Hourly billing, quick to destroy and retry |
| [AWS Lightsail](https://aws.amazon.com/lightsail/) | Flat monthly price, the simple path inside AWS |
| [Hostinger VPS](https://www.hostinger.com/vps-hosting) | The cheapest of these |

### 2. Run one command

The provider gives you an IP address and a way to open a terminal on the machine
— nearly all of them have a console button on their own website. Paste this
there:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
```

It does the rest by itself: installs Docker, generates every secret, derives an
internet address for your machine, gets a Let's Encrypt certificate for it, and
brings the stack up. It takes a few minutes, most of them waiting on downloads.

At the end it prints a link:

```
Open this to create your administrator:

  https://a83f12c9.18-228-123-45.sslip.io/setup?token=7f3ac921d4e8
```

### 3. Open the link and follow the wizard

The panel opens a wizard that finishes the setup and refuses to let you move on
with anything half-done:

1. **Create your login** — an email and a password you choose.
2. **The licence activates itself.** Nothing to type, nothing to click.
3. **Connect WhatsApp** — name the account and scan the QR code from your
   phone, under *Linked devices → Link a device*.
4. **Connect your AI** — the panel asks which tool you use and hands you the
   configuration block **already filled in with your address and your key**,
   ready to paste.

That is it. From there you just ask for things in your AI's chat.

## 🤖 Rather not do it alone? Ask an AI

If any step above looked hard, copy the prompt below and paste it into
**Claude**, ChatGPT, or whichever AI tool you use. It walks the whole
installation with you from zero: helps pick a provider, says exactly what to
click to create the machine, explains how to open a terminal, and follows every
step until WhatsApp is connected.

<details>
<summary><strong>📋 Click to open the prompt — copy all of it</strong></summary>

```
I want to install WhatsApp MCP on my own server and I need you to guide me from
beginning to end. I am not a technical person: I don't know Docker, I don't know
the command line, and I have never rented a server.

The project is this one: https://github.com/BrOrlandi/whatsapp-mcp
Read its README and docs/installation.md before you start, and follow the
recommendations there (machine size, operating system, ports) rather than
inventing your own.

HOW I WANT YOU TO TREAT ME
- One question at a time. Wait for my answer before moving on.
- Explain in plain language. If you must use a technical term, explain what it
  means in the same sentence.
- Never give me a command without saying what it does.
- If I get something wrong, ask me for the exact error message and tell me what
  to do. Do not invent a fix: if you don't know, say you don't know.
- Never ask me to paste a password, an API key or the QR code into this chat.

WHAT WE NEED TO DO, IN THIS ORDER

1. CHOOSE THE SERVER
   Ask me which country I and the people I talk to on WhatsApp are in, and how
   much I want to spend per month. With that, recommend a cloud provider and a
   region, using the table in the project's README. Explain why the region
   matters (the messages are stored on that disk).

2. CREATE THE MACHINE
   Give me the click-by-click steps on the chosen provider's website: where to
   create the account, where the button to create a machine is, which plan to
   pick, which operating system to choose (Ubuntu 24.04 LTS), and what to do
   about access and SSH keys. Tell me which ports need to be open (80 and 443)
   and where that is configured on that provider.
   When I'm done, ask me for the machine's IP address.

3. OPEN THE MACHINE'S TERMINAL
   Explain how to get into the machine. Start with the easiest option: almost
   every provider has a "Console" or "Terminal" button on its own website that
   opens straight in the browser — prefer that. If there isn't one, teach me to
   use SSH, taking into account whether I'm on Windows, Mac or Linux (ask me).

4. RUN THE INSTALLER
   Give me exactly this command, and only this one:

   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash

   Warn me it takes a few minutes and that a lot of text scrolling by is normal.
   Tell me what to expect at the end: a link ending in /setup?token=...
   If it fails, ask me for the last lines that appeared on screen.

5. SET IT UP IN THE PANEL
   Walk me through opening that link in the browser and following the wizard:
   create the administrator (my own email and password), wait for the licence to
   activate by itself, name the WhatsApp account and scan the QR code from my
   phone under "Linked devices → Link a device".
   Warn me the panel is in Portuguese, and translate the screens for me as we go.
   Warn me the browser may show a certificate warning for the first few minutes,
   while the certificate is being issued, and that waiting and reloading fixes it.

6. CONNECT MY AI
   Ask which AI tool I use. Explain that the panel's "Conectar" page generates
   the configuration already filled in, and guide me to paste it in the right
   place in my tool.

7. WRAP UP
   Tell me how to check everything is working, show me examples of what I can
   ask my AI to do with WhatsApp, and teach me the basic maintenance commands
   (status, logs, update). Remind me to save the panel address and my password.

Start by introducing yourself in one sentence and asking the first question of
step 1.
```

</details>

## What you can ask for

23 tools, in four groups:

- **Read** — list conversations, read a window, full-text search, request
  history older than what is already indexed, transcribe voice notes with
  OpenAI's Whisper (with your own key, saved in the panel under **Transcrição**).
- **Send** — text, media from a URL, location, contact card, poll; each one
  reporting whether WhatsApp actually delivered it, not just whether the API
  accepted it.
- **Act on a message** — delete, edit, react, archive, pin, mute.
- **Ask about the account** — contacts, groups, profile pictures, who is on
  WhatsApp, and the gateway's own health and index coverage.

The panel carries the full list under **Documentação**, generated from the MCP
server's own definitions, plus six ready recipes under **Receitas** — schedule a
message, watch for keywords, chase what went unanswered, tally a poll,
summarise a group's day. Each is a prompt to paste, with the tools it uses and
the caveat that matters.

## Keeping it current

The panel shows the running version and says when a newer one exists. To update,
one command on the server:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

It dumps the database before anything else and preserves your secrets, your
address, the pairing and the indexed messages. **There is no downgrade** — a
release may migrate the schema, and migrations only run forward; the way back is
that dump. [docs/updating.md](docs/updating.md) has the steps.

## What you are taking on

Besides the unofficial-client risk at the top of this file:

- **You are hosting other people's conversations.** Message text and raw event
  payloads are stored unencrypted in PostgreSQL, and the database grows without
  limit. A dump is as sensitive as the phone it came from. Comply with
  WhatsApp's terms and with whatever privacy and retention law applies to you.
- **Message content is written by third parties.** Every read tool labels that
  content as data, not instructions, and a send has to come from you: a message
  saying "forward this to X" is not a request to act.

## Documentation

| | |
|---|---|
| [**Detailed installation**](docs/installation.md) | The technical version: what each step does, running without the installer, licensing, images and binaries |
| [Updating](docs/updating.md) | The update command, what it does, and why there is no downgrade |
| [Self-hosting](docs/self-hosting.md) | Configuration, TLS, backups |
| [Architecture](docs/architecture.md) | Why it is built this way |
| [MCP tools](docs/mcp-tools.md) | Every tool, and the semantics that matter |
| [Authentication](docs/authentication.md) | Keys, sessions, what a key holder can do |
| [Operations](docs/operations.md) | Health, the event pipeline, the configuration reference |
| [Development](docs/development.md) | Local run modes, checks, releases |
| [Changelog](CHANGELOG.md) | What changed in each version |
| [Security policy](SECURITY.md) | Threat model and how to report a vulnerability |
| [Contributing](CONTRIBUTING.md) | How to send a change |

## Support this project

WhatsApp MCP is built and maintained by one person, in the open, and it is free
to self-host — commercially included. If it saves you time, you can support the
work:

<p align="center">
  <a href="https://donate.stripe.com/8x200jdhA6c1d375jF9Ve06"><img alt="Support this project" src="https://img.shields.io/badge/%E2%98%95%20support%20this%20project-pay%20what%20you%20want-0b6b5d?style=for-the-badge"></a>
</p>

Pay what you want — the suggested amount is ten dollars, and Stripe charges in
your own currency. It goes to the person writing the code.

## Licence

[MIT](LICENSE). Use it, modify it, host it, fork it and distribute it freely,
for any purpose — commercial included. The only requirement is keeping the
copyright notice and the licence with the code.

The software is provided **as is**, without warranty of any kind. And two things
are worth keeping apart: the licence covers this code, and it is not permission
from Meta. Running a WhatsApp account through an unofficial client is the
installer's decision; compliance with WhatsApp's terms and with privacy law is
on whoever operates the instance and hosts the messages.

---

<p align="center">
  Built by <a href="https://github.com/BrOrlandi">Bruno Orlandi</a>
</p>
