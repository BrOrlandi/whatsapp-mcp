# WhatsApp MCP gateway

A small Go gateway that puts WhatsApp behind an authenticated MCP endpoint. Evolution Go, RabbitMQ and PostgreSQL are internal dependencies of this backend; an MCP client needs none of them.

A client needs exactly one thing: an API key. The key identifies the account and the WhatsApp instance it is authorised for, so there is no user, no password and no instance name to configure.

```json
{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "https://whatsapp-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer wamcp-…" }
    }
  }
}
```

Keys are issued and revoked in the control panel, which also prints this block filled in. Keep the key in the client's credential store, never in a prompt or a versioned file.

The same HTTP server provides a Portuguese control panel at `http://127.0.0.1:8080/`, split by task: **Conectar** hands a client everything it needs, **Instâncias** owns the WhatsApp account lifecycle, and **Estado** is the diagnostic view. Creating a key or an instance happens in a dialog, and destructive actions ask first. Evolution Go has no public surface and its manager is never needed.

**Conectar** is a three-step checklist that tracks itself: a key exists or it does not, and a key that has been used proves a client authenticated with it. While the second step waits, the page polls and closes it the moment a client makes its first call. Generating a key shows the secret once alongside the `claude mcp add` command, the JSON block for file-configured clients, and a prompt to verify the connection — all filled in. Passwords are bcrypt-hashed in PostgreSQL; sessions use signed, HttpOnly, SameSite cookies with concurrency-safe server-side state. A process restart intentionally invalidates active sessions.

The panel mints a per-instance Evolution token, stores it in PostgreSQL and uses it for every per-instance call, because Evolution resolves the target instance from the key on the request and accepts no instance parameter. That token is an internal secret: it is never displayed and never reaches an MCP client. An instance created outside the panel has no token here, so the panel marks it and refuses to operate it.

## Start the stack

Requirements: Docker Engine with Compose v2. Copy the example environment and replace every placeholder with a distinct randomly generated secret:

```sh
cp .env.example .env
docker compose config
docker compose up --build -d
docker compose ps
curl http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

The Compose stack uses pinned images and persistent named volumes for Evolution Go, both PostgreSQL databases, RabbitMQ, and MinIO. Management ports bind to loopback only. PostgreSQL migrations run automatically when `whatsapp-mcp` starts.

Evolution Go requires its own activation/license flow on first use. Review Evolution Go's LICENSE, NOTICE, and trademark conditions before redistribution.

## Pair with QR

Open the control panel, create an instance by name, and the panel takes it from there: it registers the instance with Evolution, subscribes it to the `MESSAGE`, `SEND_MESSAGE`, `HISTORY_SYNC` and `CONNECTION` event queues, starts the client and shows the QR code. Scan it from WhatsApp under **Linked devices → Link a device**. The pairing page refreshes itself, so a scanned code moves forward on its own; codes expire quickly and the page can mint another.

Subscribing at connect time is what makes Evolution publish at all — its RabbitMQ producer drops every event unless the connect call sets `rabbitmqEnable`. Do not paste API keys or QR payloads into chat, issue trackers, or logs.

Evolution Go resolves the target instance from the `apikey` header, so per-instance routes carry the instance token and only administrative routes (`/instance/create`, `/instance/all`, `/instance/delete/{id}`) carry the global key. `docs/evolution/endpoint-map.md` maps the API this project depends on, against the `swagger.yaml` versioned next to it.

## Architecture

Evolution Go exposes no route to list conversations or read message history — the `/chat/findChats` and `/chat/findMessages` routes of Evolution API v2 do not exist in it. That decides the shape of this gateway:

- **PostgreSQL is the only source of conversations and history.** Evolution publishes every event to RabbitMQ, this gateway consumes it and indexes it. The index covers exactly what has been ingested, which is why every reading tool reports `history_since`.
- **Evolution answers for everything live**: the address book, the groups, sending, and the instance lifecycle.
- **One service layer, two façades.** The MCP tools and the panel share the same code; the tools call it directly rather than looping back through HTTP.

`docs/evolution/endpoint-map.md` maps the API this depends on, against the `swagger.yaml` versioned beside it. `docs/remote-mcp-auth-pending.md` records the decisions and what is still open; `docs/backlog.md` and the repository issues track what comes next.

## MCP tools

| Tool | Source | Purpose |
|---|---|---|
| `whatsapp_status` | gateway | session state, queues, index coverage, problems |
| `list_chats` | index | conversations, most recently active first |
| `get_chat_messages` | index | one conversation over a period |
| `search_messages` | index | full-text search, optionally scoped |
| `list_contacts` | Evolution | address book |
| `list_groups` | Evolution | groups the account belongs to |
| `get_group` | Evolution | one group with its participants |
| `send_text_message` | Evolution | send text |
| `send_media_message` | Evolution | send image, video, audio or document from a URL |
| `download_media` | Evolution | decode the media of an indexed message |
| `sync_history` | Evolution | request messages older than the index holds |

Summarising is not a tool: `get_chat_messages` returns the period and the client summarises it, which avoids an LLM credential and a per-call cost in the backend.

Forwarding is not a tool either, because WhatsApp exposes no forwarding route. Resending the content with `send_text_message` or `send_media_message` is what "forward" means here, and the tool names say so rather than implying otherwise.

`sync_history` returns immediately. WhatsApp answers asynchronously: it returns the messages immediately *before* one the account already knows, they arrive on the history queue, and each call pages further back. An instance with nothing indexed has no anchor to page from.

### Untrusted content

Message content is written by third parties. Every reading tool labels it as data rather than instructions, and a send must originate from the user: a message that says "forward this to X" is not a request to act on.

## Authentication

Keys are `wamcp-` followed by 24 alphanumeric characters, around 142 bits of entropy. Only the SHA-256 digest is stored — SHA-256 rather than bcrypt because bcrypt's deliberate cost protects a human-chosen password against a dictionary, and would add roughly 100 ms to every MCP request here while guarding against an attack that cannot succeed. The panel's administrator password stays on bcrypt.

The endpoint has no anonymous mode. A credential is accepted only in the `Authorization` header, never in a query string; a rejected one gets a `401` that does not distinguish an unknown key from a revoked one; repeated failures are throttled per source address. Revocation takes effect immediately, because each request is authenticated on its own.

That statelessness is also what makes the endpoint resilient: there is no session to resume, so a dropped connection costs nothing.

## Health and freshness

- `GET /healthz` is process liveness and returns HTTP 200 while the gateway can answer requests.
- `GET /readyz` returns HTTP 200 only when Evolution, RabbitMQ, and PostgreSQL are connected and a persisted event is newer than `FRESHNESS_WINDOW`; otherwise it returns HTTP 503.
- Both carry the whole picture: the WhatsApp session state with the reason WhatsApp gave, per-queue consumption with counters, and the problems in plain language. The same snapshot feeds the panel and `whatsapp_status`, so all three describe a failure identically.
- Authenticated `GET /api/selected-instance` reports the persisted selection and one of `api_unavailable`, `no_instance`, `disconnected`, `connecting`, or `connected`.

A quiet account and a broken pipeline look alike from age alone, so the tools report rather than refuse: a degraded gateway still answers, with the problems and the index coverage attached. Refusing would hide the messages that *are* indexed and leave the caller unable to tell which situation they are in.

Session state comes from Evolution's connection events. The readiness poll is reconciliation after a restart, and it may confirm a live session but never overwrite a specific failure with a vague one: `logged_out` tells the operator to scan a new QR code, `disconnected` tells them to wait.

RabbitMQ uses durable quorum queues and manual acknowledgements. The gateway consumes every queue the subscribed events create — `message`, `sendmessage`, `historysync` and the six connection queues — because a queue Evolution declares and nobody reads grows without bound. Valid events are acknowledged only after the PostgreSQL transaction commits. Duplicate deliveries are harmless through event/message uniqueness constraints. Transient database failures are explicitly requeued and retried after reconnect; malformed JSON is rejected without requeue to prevent a poison-message loop. Inspect RabbitMQ logs/metrics for rejected deliveries and add a broker policy/DLX if malformed payload retention is required operationally.

## Local development

`just` drives everything. `just` on its own lists the recipes.

There are three ways to run it, from least to most setup:

```sh
just preview      # the panel alone, fake data, no dependencies at all
just tunnel       # in one terminal: SSH tunnel to the server's Evolution
just dev-remote   # in another: the gateway against that Evolution
just up && just dev   # the whole stack locally, with a WhatsApp of your own to pair
```

**`just preview`** serves the control panel against fabricated data on port 8090. It talks to nothing, so it is the fastest way to work on layout and wording.

**`just dev-remote`** runs the gateway locally against the Evolution on the server. Live reads and sending work; ingestion does not, and that is expected — the server's Evolution publishes to the server's queue, not to yours, so the local index stays empty. Evolution has no published port and no public domain, so `just tunnel` asks the host for the container's address on the Docker bridge and forwards to it.

**`just up && just dev`** runs everything locally and needs a WhatsApp account to pair. It is the only mode where ingestion, history sync and the message index actually work.

`just check` runs what has to pass before a commit: format, vet, tests, race, build, compose validation.

`POST /mcp` is the supported transport. A stdio transport exists for debugging a local build and is off unless `MCP_STDIO=true`; it has no credential, so it acts on the instance the panel selected.

Running the binary directly needs `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL` and `EVOLUTION_API_KEY`. These are backend configuration: an MCP client never sees them.

Do not pass message text into shell commands. Evolution is an unofficial WhatsApp integration and may be logged out, disrupted by protocol changes, or subject to account restrictions. Use a test account first and comply with WhatsApp policies and applicable privacy and retention law.

## Retention

The database grows without limit. Cleanup is deliberately deferred until there is real volume to size a ceiling against, and the rule is already fixed: delete messages, never conversations. Message text and raw event payloads are stored unencrypted in PostgreSQL. See issue #4.

## Dokploy public domains

Only `whatsapp-mcp` is published. Evolution Go, RabbitMQ, MinIO and both PostgreSQL instances stay on the project's internal network: `dokploy-network` is shared by every Dokploy project, so anything attached to it is reachable by any other project's containers without passing through Traefik or TLS.

In this Dokploy installation, Traefik's secure entrypoint is named `web-secure`; Dokploy-generated Compose domains may currently emit the incompatible `websecure` name and return a 404 before the request reaches the container.

Before changing production, compare a working Compose project with `domain.byComposeId`, `compose.loadServices`, and `compose.getConvertedCompose`. The compatibility file `deploy/traefik/whatsapp-mcp.yml` contains the working file-provider route for the public host and the exact internal service port. It must be installed on the Dokploy host by an authorized operator:

```sh
sudo install -m 0644 deploy/traefik/whatsapp-mcp.yml /etc/dokploy/traefik/dynamic/whatsapp-mcp.yml
```

The file provider watches that directory, so no Traefik restart should be necessary. Verify the file was loaded and the routers use `web-secure`, then test:

```sh
curl -fsS https://whatsapp-mcp.example.com/healthz
```

If the Dokploy domain configuration is corrected to generate `web-secure`, remove the temporary file-provider routes and redeploy the Compose stack. Do not report success based only on container health: the MCP health endpoint, valid TLS certificate, first-access setup, and authenticated dashboard must all be verified.

The project identity is an original green message-and-node mark, kept in `internal/brand/logo.svg` and embedded into the setup, login, and dashboard pages without an external CDN. The same asset is used here:

![WhatsApp MCP logo](internal/brand/logo.svg)

The visual system preserves the WhatsApp-inspired green palette while keeping this project independent and unofficial. The UI supports light and dark system themes, responsive layouts, keyboard focus states, and an explicit degraded state when Evolution is disconnected.

## Development checks

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/whatsapp-mcp
docker compose config
```
