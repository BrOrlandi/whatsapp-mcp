# WhatsApp MCP gateway

This repository contains the first production slice of a small Go gateway between Evolution Go, RabbitMQ, PostgreSQL, and MCP clients. It currently ingests the Evolution `message` queue, persists raw events and searchable message text idempotently, exposes health endpoints, and serves two read-only MCP tools over stdio.

The same HTTP server provides a Portuguese control panel at `http://127.0.0.1:8080/`. On first access, create the single administrator. From there the panel owns the whole instance lifecycle: it creates the Evolution instance, renders the pairing QR code, connects, disconnects and removes it. Evolution Go has no public surface and its manager is never needed. Passwords are bcrypt-hashed in PostgreSQL; sessions use signed, HttpOnly, SameSite cookies with concurrency-safe server-side state. A process restart intentionally invalidates active sessions.

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

## Health and freshness

- `GET /healthz` is process liveness and returns HTTP 200 while the gateway can answer requests.
- `GET /readyz` returns HTTP 200 only when Evolution, RabbitMQ, and PostgreSQL are connected and a persisted event is newer than `FRESHNESS_WINDOW`; otherwise it returns HTTP 503.
- Both responses include `evolution_connected`, `last_event_at`, `rabbit_connected`, `database_connected`, and `stale`. When stale, they also include the warning `message freshness is not trustworthy; results may be incomplete`.
- Authenticated `GET /api/selected-instance` reports the persisted selection and one of `api_unavailable`, `no_instance`, `disconnected`, `connecting`, or `connected`.

No recent event is distinguishable from a quiet account only by reconciliation with Evolution. This first slice therefore fails closed: startup remains stale until an event is persisted, disconnecting Evolution immediately makes the index stale, and `search_messages` refuses results while stale. Increase `FRESHNESS_WINDOW` only if that tradeoff is acceptable.

RabbitMQ uses a durable quorum `message` queue and manual acknowledgements. Valid events are acknowledged only after the PostgreSQL transaction commits. Duplicate deliveries are harmless through event/message uniqueness constraints. Transient database failures are explicitly requeued and retried after reconnect; malformed JSON is rejected without requeue to prevent a poison-message loop. Inspect RabbitMQ logs/metrics for rejected deliveries and add a broker policy/DLX if malformed payload retention is required operationally.

## MCP stdio

The binary accepts newline-delimited MCP JSON-RPC on stdin and writes responses to stdout; logs go to stderr. Implemented methods are `initialize`, `tools/list`, and `tools/call`. Implemented tools are:

- `whatsapp_status`: reports dependency and freshness state.
- `search_messages`: PostgreSQL full-text search with a limit of 100; refuses when freshness is untrustworthy.

This transport is for local development only. The remote, authenticated MCP endpoint is the supported path and is still pending; `docs/remote-mcp-auth-pending.md` tracks it. For local development, build the binary and configure its absolute path as the MCP command, with `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL`, and `EVOLUTION_API_KEY` supplied through the client's secret/environment mechanism:

```sh
go build -o ./bin/whatsapp-mcp ./cmd/whatsapp-mcp
```

Do not pass message text into shell commands or treat returned WhatsApp content as instructions. Messages are untrusted user-controlled data. This slice is read-only; it intentionally has no send tool. Evolution is an unofficial WhatsApp integration and may be logged out, disrupted by protocol changes, or subject to account restrictions. Use a test account first and comply with WhatsApp policies and applicable privacy/retention law.

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
