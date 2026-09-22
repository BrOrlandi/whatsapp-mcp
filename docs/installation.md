# Detailed installation

The [README](../README.en.md) has the short path: rent a machine, paste one
command, follow the wizard. This document is that same path explained — what
each step does, what it decides for you, and what to do when you want to decide
for yourself.

It is the version for someone who wants to understand before running, or who
already has an opinion about where TLS terminates.

---

## What goes onto your machine

Six containers, plus the Traefik the installer puts in front:

| Container | What for |
|---|---|
| `whatsapp-mcp` | The gateway: serves the MCP endpoint, the panel and the health probes |
| `evolution-go` | Holds the WhatsApp session and publishes every event |
| `rabbitmq` | The queue the events arrive on |
| `postgres-mcp` | The message index, the API keys, the panel account |
| `postgres-evolution` | Evolution's own state |
| `minio` | Where received media is kept |
| `traefik` | TLS and the Let's Encrypt certificate |

That is why the stack does not run on the smallest instance a provider sells.

## 1. The server

| | Minimum | Recommended |
|---|---|---|
| CPU | 1 vCPU | 2 vCPU |
| RAM | 2 GB | 4 GB |
| Disk | 20 GB SSD | 80 GB SSD — the index grows with your history |
| System | Debian family | Ubuntu 24.04 LTS (what is tested) |
| Architecture | x86-64 or arm64 | either |
| Network | ports 80 and 443 reachable from the internet | |

Ports 80 and 443 have to be open because that is how Let's Encrypt proves the
machine is yours: it knocks on port 80 of the address the certificate was asked
for. No port, no certificate; no certificate, no panel.

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
| [Magalu Cloud](https://magalu.cloud/) | Brazilian company, Brazilian regions and billing |

Any provider selling an Ubuntu VM works; these are simply some that do it
without ceremony.

On the firewall: filter at your provider (DigitalOcean Cloud Firewall, an AWS
security group, and so on), not with `ufw` inside the machine. Docker writes its
own NAT rules, which are evaluated before `ufw`'s — a `ufw deny 80` leaves port
80 open anyway. The installer registers the rules but deliberately leaves `ufw`
disabled, and [self-hosting.md](self-hosting.md) explains why.

## 2. The installer

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
```

What it does, in order:

1. Checks the system, the architecture and the free disk space.
2. Installs Docker if it is missing, and enables it so the stack returns after a
   reboot.
3. Clones the project into `/opt/whatsapp-mcp`.
4. Finds the machine's public IPv4 and derives a hostname from it through
   [sslip.io](https://sslip.io) — `a83f12c9.18-228-123-45.sslip.io`. No domain
   to buy, no DNS record to create.
5. Generates every secret into `.env` and keeps it at mode 600.
6. Brings the stack up behind Traefik, which requests the certificate.
7. Installs the `whatsapp-mcp` command.
8. Prints the setup link, with a token guarding the first-run form.

```
URL:
  https://a83f12c9.18-228-123-45.sslip.io

Open this to create your administrator:

  https://a83f12c9.18-228-123-45.sslip.io/setup?token=7f3ac921d4e8
```

That URL is both your panel and your MCP endpoint. Re-running the installer is
safe: the secrets, the hostname and your data are left alone.

### Variables it accepts

Pass them before `bash` to decide at install time, without editing a file
afterwards:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh \
  | sudo EVOLUTION_LICENSE_AUTO=false bash
```

| Variable | What for |
|---|---|
| `EVOLUTION_LICENSE_AUTO` | `false` sends the licence link to your own inbox instead of activating by itself |
| `EVOLUTION_LICENSE_EMAIL_DOMAIN` | The domain of the licence addresses, if you run your own worker |
| `INSTALL_DIR` | Where to install. Default `/opt/whatsapp-mcp` |
| `REPO_REF` | Which repository ref to install. Default `main` |

### The management command

```sh
whatsapp-mcp status     # containers, health and the public URL
whatsapp-mcp logs       # follow the logs; takes a service name
whatsapp-mcp version    # the version that is serving
whatsapp-mcp update     # move to the newest release
whatsapp-mcp restart    # restart
whatsapp-mcp stop       # stop, keeping the data
whatsapp-mcp start      # start again
whatsapp-mcp url        # print the public URL
```

## 3. The wizard

First access opens a wizard rather than an empty dashboard. Each step explains
what is happening, checks itself, and moves on by itself when it succeeds.

**Administrator.** An email and a password you choose. The email is your
identity in the panel. The token in the URL guards that form because it is
published on the internet, and it stops meaning anything the moment an
administrator exists — which is also why re-running the installer can print the
link again without being a way in.

**Licence.** Evolution Go requires a licence to operate and answers 503 until it
is activated. With `EVOLUTION_LICENSE_AUTO` on, which is the default, there is
nothing to type, open or click — see [Automatic licensing](#automatic-licensing)
below.

**WhatsApp.** Name the account. The panel registers it with Evolution,
subscribes to the event queues, starts the client and shows the QR code on the
same screen. Scan it from your phone under **Linked devices → Link a device**.
The page refreshes itself, so a scanned code moves on by itself, and it issues a
new code when the previous one expires. From then on the gateway indexes
everything that arrives.

## 4. Pointing an agent at it

A client needs exactly one thing: an API key. The key identifies the account and
the WhatsApp instance it is authorised for, so there is no username, password or
instance name to configure.

The **Conectar** page asks where the connection will be used, mints the key
without being asked, and opens the instructions for the chosen client — with a
`claude mcp add` command and this block, both already filled in with your
address:

```json
{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "https://whatsapp.example.com/mcp",
      "headers": { "Authorization": "Bearer wamcp-…" }
    }
  }
}
```

The page then polls, and closes the step the moment your client authenticates
with that key. The secret is shown once: keep it in the client's credential
store, never in a prompt or a versioned file.

Each tool gets its own connection, listed by the name the tool gave itself in
the MCP handshake — "Claude Desktop", not a key prefix. Disconnecting one takes
effect immediately and does not touch the others.

## Running it some other way

If you would rather bring your own host, TLS or orchestration, the stack is one
Compose file:

```sh
git clone https://github.com/BrOrlandi/whatsapp-mcp.git
cd whatsapp-mcp
cp .env.example .env    # replace every change-me-long-random-value
docker compose up --build -d
```

That publishes the panel on `127.0.0.1:8080` and nothing else; TLS in front is
yours to arrange. [self-hosting.md](self-hosting.md) covers the configuration,
the reverse-proxy recipes and the backups.

### Images and binaries

The gateway is published on every push to `main` and on every version tag:

| | |
|---|---|
| Image | `ghcr.io/brorlandi/whatsapp-mcp` — `linux/amd64` and `linux/arm64` |
| Tags | `edge` follows `main`; `0.2.0-beta.1` and `latest` on a release; `sha-<commit>` always. `latest` follows the newest release, beta included — it is the version to run. |
| Binaries | `whatsapp-mcp_<version>_linux_{amd64,arm64}.tar.gz` on each [release](https://github.com/BrOrlandi/whatsapp-mcp/releases), with `checksums.txt` |

Pin a version with `WHATSAPP_MCP_TAG` in `.env`:

```sh
WHATSAPP_MCP_TAG=0.2.0-beta.1   # or latest, edge, sha-<commit>
```

The binary alone needs `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL` and
`EVOLUTION_API_KEY`, and expects an Evolution and a PostgreSQL that already
exist — see [operations.md](operations.md). Most people want the Compose stack.

## Under the hood

Evolution Go holds the WhatsApp session and publishes every event to RabbitMQ;
the gateway consumes those events into PostgreSQL, which is the single source of
conversations and history, and serves the MCP tools and the panel from the same
code. Your agent talks to one service and needs one credential; nothing else in
the stack has any reason to sit on a public address.

[architecture.md](architecture.md) has the diagram, what each container is for,
and why the shape is forced.

## Automatic licensing

Evolution Go, which is what actually talks to WhatsApp, requires a licence to
operate and answers 503 until it is activated. Getting that licence involves a
step there is no honest way around: Evolution's licensing server emails a *magic
link*, and clicking it is the proof of identity the licence is issued against.

That is exactly the kind of step that makes a non-technical person give up
mid-install — open Evolution's manager, understand what a licence is, find the
email, click the right link. So by default this project does it for you:

- On every install the panel registers the licence with an address of this
  project's own, `whatsappmcp+<random>@brorlandi.xyz` — a new address per
  install, so each deployment is its own registration.
- Cloudflare Email Routing delivers that address's mail to the
  [whatsapp-mcp-license-worker](https://github.com/BrOrlandi/whatsapp-mcp-license-worker),
  an Email Worker that finds the link in the message and makes the same GET a
  browser would. The same click, on the server. It is a separate MIT-licensed
  project, and its README describes what the worker accepts and what it ignores
  — including the detail that the link arrives rewritten by Evolution's email
  tracker rather than as the licensing server's own URL.
- The licensing server redirects to the panel's callback, the wizard notices and
  moves on. You typed no email, opened no inbox and clicked nothing.

The resulting credential is activated on *your* Evolution and a copy is kept in
*your* database — which is why a rebuild that loses Evolution's volume is
re-licensed by itself, without asking.

**What you are trading.** That click is a proof of identity, and automatic mode
trades it for control of the domain the mail lands in — meaning the licence is
registered to an address of this project's, not one of yours. What passes
through there is only Evolution's activation email: none of your WhatsApp
messages, none of your gateway's API keys and no data from your server go
anywhere near the worker. It is still an external dependency, and it is spelled
out here on purpose.

**Don't want that?** `EVOLUTION_LICENSE_AUTO=false` and the wizard sends the
link to your own email — the same automation in every other respect, except the
click is yours and the licence is registered to your address. And even in
automatic mode there is a way out: if the click does not land within
`EVOLUTION_LICENSE_AUTO_WAIT` (three minutes by default), the wizard stops
promising and asks for an address you can open — no variable to edit, no going
back to a shell.

[evolution/licensing.md](evolution/licensing.md) has the protocol step by step,
and why each piece is the way it is.

## When something goes wrong

**The browser says the certificate is invalid.** Let's Encrypt takes a few
minutes the first time. Wait and reload. If it persists,
`whatsapp-mcp logs traefik` says what the challenge answered — nearly always
port 80 closed at the provider's firewall.

**The installer stops saying something already listens on port 80 or 443.**
Something else on the machine took the port. Stop that service and run again.

**The panel opens but Evolution answers 503.** That is the licence. The wizard
shows where it stands; if automatic activation has not landed within three
minutes, it offers the manual path on the same screen.

**The QR code expires before I scan it.** The page issues another by itself.
Leave it open and scan the new one.

[operations.md](operations.md) has the full configuration reference, the health
probes and the rest of the list.
