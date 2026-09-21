# Self-hosting

Everything runs from one Compose file. A host with Docker Engine and Compose v2,
about 2 GB of RAM and a WhatsApp account you are willing to link is enough.

## 1. Configure

```sh
git clone https://github.com/BrOrlandi/whatsapp-mcp.git
cd whatsapp-mcp
cp .env.example .env
```

Replace **every** `change-me-long-random-value` in `.env` with a distinct random
secret. One command that does it on macOS and Linux:

```sh
while grep -q change-me-long-random-value .env; do
  python3 - <<'PY'
import re, secrets, pathlib
p = pathlib.Path(".env")
p.write_text(re.sub("change-me-long-random-value", lambda _: secrets.token_urlsafe(32), p.read_text(), count=1))
PY
done
```

Then set the value that describes your host:

| Variable | What it is |
|---|---|
| `PUBLIC_URL` | The address MCP clients will use, e.g. `https://whatsapp.example.com`. The panel prints it inside the ready-to-paste client configuration, so getting it wrong produces a config that does not connect. Leave the default `http://127.0.0.1:8080` if you only ever reach the gateway from the machine it runs on. |

`.env` is in `.gitignore` and must stay there.

## 2. Start

```sh
docker compose config          # validates .env is complete
docker compose up -d           # pulls ghcr.io/brorlandi/whatsapp-mcp:edge
docker compose ps
curl http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

`/readyz` answers 503 until an instance is paired and events are flowing. That
is the expected state on a fresh install, not a failure.

The gateway image comes from GHCR rather than being compiled here, so a 1 GB
machine is enough and a deploy is a pull. `WHATSAPP_MCP_TAG` in `.env` pins it
(`edge` follows `main`, `v1.2.3` holds still); `docker compose up -d --build`
compiles the checkout instead, which is what you want when working on the code.

Images are pinned and every database uses a persistent named volume. The
RabbitMQ and MinIO management ports bind to loopback only. The gateway's own
PostgreSQL migrations run automatically when it starts.

## 3. Activate Evolution Go

[Evolution Go](https://github.com/EvolutionAPI/evolution-go) requires a licence
to operate: until it is activated, its endpoints answer 503. Activation is a
wizard step the panel itself drives, and by default it needs nothing from you:
with `EVOLUTION_LICENSE_AUTO` on (the default), the panel registers the licence
with this project's own address (`whatsappmcp+…@brorlandi.xyz`), whose mail is
read by [the licence worker](https://github.com/BrOrlandi/whatsapp-mcp-license-worker)
— the link gets clicked, the wizard notices the licence and moves on by itself.
Nobody types an email, opens an inbox, or opens Evolution's own manager. See
[evolution/licensing.md](evolution/licensing.md) for what the licence is and
how each piece of this works.

Prefer your own inbox? Set `EVOLUTION_LICENSE_AUTO=false` before install (or
in `.env`, then `whatsapp-mcp restart`) and the wizard sends the activation link
to your email instead — same automation, except the one click on the emailed
link is yours. That click is the identity proof the licence is issued for;
the automatic mode trades it for control of the domain the mail lands on.

### The startup fallback

Evolution Go also documents a headless startup activation: an email that has
registered before, put in `EVOLUTION_OPERATOR_EMAIL`, makes Evolution call
their licensing server itself on boot with no browser step.

```sh
EVOLUTION_OPERATOR_EMAIL=you@example.com
```

It is belt and braces rather than the main path — as of this writing the live
licensing server answers that route with 401 "missing token" for a request
that Evolution Go 0.7.2 sends no header for, so it may be dead until they
reconcile versions. The panel's own flow does not depend on it: every rebuild
that loses the Evolution volume is re-licensed from the copy the panel keeps,
and every reinstall registers again by itself in the wizard.

Evolution Go is Apache-2.0 with additional brand-protection conditions, and
"Evolution", "Evolution Go" and "Evolution Foundation" are trademarks. Review
its `LICENSE`, `NOTICE` and `TRADEMARKS.md` before redistributing anything that
bundles it.

## 4. First access and pairing

Open the panel — `http://127.0.0.1:8080/` by default. The first visit asks you
to create the administrator account: an email and a password, no default
credentials to forget to change. The email is the identity you sign in with,
and it has to be one you can open — the installation's next step sends a
confirmation to it.

On a panel that is reachable from the internet, that form needs guarding: in the
seconds between the address existing and you reaching it, whoever arrives first
becomes the administrator of your WhatsApp session. `SETUP_TOKEN` in the
environment is what closes that — the form then opens only at
`/setup?token=<the token>` and refuses everything else. `install.sh` generates
one and prints the finished link, so there is nothing to copy. Set it yourself
if you publish the panel some other way:

```sh
SETUP_TOKEN=$(openssl rand -hex 12)
```

Leave it empty for a panel bound to loopback, where there is no race to lose.

Signing in then opens the installation wizard, which asks for the licence
first and for the WhatsApp account second — one screen at a time, and no panel
chrome to wander off into until the gateway works. Name the account and the
panel registers it with Evolution, subscribes it to the `MESSAGE`,
`SEND_MESSAGE`, `HISTORY_SYNC` and `CONNECTION` event queues, starts the client
and shows the QR code. Scan it from WhatsApp under **Linked devices → Link a
device**. The page refreshes itself, so a scanned code moves forward on its
own; codes expire quickly and the page mints another.

The wizard is the first run only. A deployment that has already issued a key is
past it, so a later outage lands on the panel — where **Estado** and the
instance controls are — instead of on an install screen that cannot help.

Do not paste API keys or QR payloads into chat, issue trackers, or logs.

## 5. Issue a client key

**Conectar** is a three-step checklist that tracks itself. Generating a key shows
the secret once, alongside the `claude mcp add` command and the JSON block for
file-configured clients, both filled in. The page then polls and closes the step
the moment a client authenticates with that key.

Keep the key in the client's credential store, never in a prompt or a versioned
file. See [authentication](authentication.md) for what the key is and how
revocation behaves.

## Putting it on the internet

The gateway publishes its port on loopback by default, and that default is the
safe one: the panel is an administrative interface for a linked WhatsApp
account. If you expose it, expose it behind TLS.

- **Reverse proxy.** Terminate TLS in Caddy, nginx, Traefik or your platform's
  ingress and forward to the container port `8080`. Set `PUBLIC_URL` to the
  public address. A minimal Caddyfile is two lines:

  ```
  whatsapp.example.com {
      reverse_proxy 127.0.0.1:8080
  }
  ```

- **Binding.** `PANEL_BIND` and `PANEL_PORT` control the published port if you
  need something other than `127.0.0.1:8080`. `PANEL_BIND=0.0.0.0` puts the
  panel on every interface, which only makes sense on a host nobody else
  reaches.
Never publish Evolution Go, RabbitMQ, MinIO or either PostgreSQL. The gateway is
the only service that belongs on a public address.

## Ports and the firewall

**The application needs exactly two public ports: 80 and 443.** Everything else
should be closed, including everything this stack runs internally.

| Port | Why it has to be public |
|---|---|
| `443/tcp` | The panel and the MCP endpoint. This is the one clients use. |
| `80/tcp` | Let's Encrypt answers its HTTP challenge here, so the certificate cannot be issued or renewed without it. It also redirects to 443; nothing is served in the clear. |
| your SSH port | Not the application's — yours. Keep it reachable only from where you administer the machine. |

Nothing else belongs on a public address. Evolution Go, RabbitMQ, MinIO and both
PostgreSQL instances stay on the Compose network; the RabbitMQ and MinIO
management ports bind to `127.0.0.1` and are reachable only through an SSH
tunnel.

### Filter at your provider, not with ufw

Which tool you use depends on the provider — DigitalOcean Cloud Firewall, AWS
security groups, Hetzner Firewall, Oracle security lists — but the rule is the
same everywhere: allow 80, 443 and your SSH port, deny the rest.

Do that at the provider rather than with `ufw` on the machine, because **`ufw`
does not govern a published container port.** Docker writes its own DNAT rules
into the `nat` table, which is evaluated before ufw's filter rules, so
`ufw deny 80` leaves port 80 wide open. The only thing ufw would actually filter
on a host like this is `sshd`, which is the one service you need. `install.sh`
registers the ufw rules so enabling it later cannot lock you out, and leaves it
inactive on purpose: a firewall that appears to protect the published ports and
does not is worse than none.

A provider firewall sits in front of the machine, where Docker has no way to
route around it.

## Upgrading

```sh
git pull
docker compose up -d --pull always
```

Migrations run on start. Volumes are preserved, so the pairing and the indexed
messages survive. `docker compose down` stops the stack without touching them;
`docker compose down -v` deletes them, which means re-pairing and re-syncing.

## Backups

Two things are worth backing up, and they are not the same thing:

- `postgres_mcp_data` — the message index, the API keys, the panel account and
  the instance registry. Lose it and you lose the searchable history.
- `evolution_data` and `postgres_evolution_data` — the WhatsApp session. Lose
  them and you re-scan a QR code.

`docker compose exec postgres-mcp pg_dump -U "$MCP_DB_USER" "$MCP_DB_NAME"` is
enough for the first; the volumes themselves are the practical answer for the
second. Message text and raw event payloads are stored unencrypted, so treat a
dump exactly as you would treat the phone.

## Limits worth knowing before you start

Evolution Go is an unofficial WhatsApp integration. It may be logged out,
disrupted by protocol changes, or subject to account restrictions. Use a test
account first, and comply with WhatsApp's terms and with whatever privacy and
retention law applies to you — you are hosting other people's conversations.

The database grows without limit; see [operations](operations.md#retention).
