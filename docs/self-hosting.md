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
docker compose up --build -d
docker compose ps
curl http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

`/readyz` answers 503 until an instance is paired and events are flowing. That
is the expected state on a fresh install, not a failure.

Images are pinned and every database uses a persistent named volume. The
RabbitMQ and MinIO management ports bind to loopback only. The gateway's own
PostgreSQL migrations run automatically when it starts.

## 3. Activate Evolution Go

[Evolution Go](https://github.com/EvolutionAPI/evolution-go) requires a licence
to operate: until it is activated, its endpoints answer 503 and the panel will
report Evolution as unavailable. Activation happens once, in Evolution's own
manager, which the Compose stack exposes on the internal network. Follow the
activation flow documented in the Evolution Go repository.

Evolution Go is Apache-2.0 with additional brand-protection conditions, and
"Evolution", "Evolution Go" and "Evolution Foundation" are trademarks. Review
its `LICENSE`, `NOTICE` and `TRADEMARKS.md` before redistributing anything that
bundles it.

## 4. First access and pairing

Open the panel — `http://127.0.0.1:8080/` by default. The first visit asks you
to create the administrator account; there is no default password to forget to
change.

Then, in **Instâncias**, create an instance by name. The panel registers it with
Evolution, subscribes it to the `MESSAGE`, `SEND_MESSAGE`, `HISTORY_SYNC` and
`CONNECTION` event queues, starts the client and shows a QR code. Scan it from
WhatsApp under **Linked devices → Link a device**. The pairing page refreshes
itself, so a scanned code moves forward on its own; codes expire quickly and the
page can mint another.

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

## Upgrading

```sh
git pull
docker compose up --build -d
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
