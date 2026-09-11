# WhatsApp MCP gateway

This repository contains the first production slice of a small Go gateway between Evolution Go, RabbitMQ, PostgreSQL, and MCP clients. It currently ingests the Evolution `message` queue, persists raw events and searchable message text idempotently, exposes health endpoints, and serves two read-only MCP tools over stdio.

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

Evolution Go requires its own activation/license flow. Open `http://127.0.0.1:4000/manager/login`, use the API key from your local `.env`, and complete activation. Review Evolution Go's LICENSE, NOTICE, and trademark conditions before redistribution.

## Pair with QR

After activation, create an instance through the Evolution manager or its documented instance API. Enable RabbitMQ for the instance and subscribe to `MESSAGE`; the stack also enables the global `message` event queue. In the manager, request the QR code and scan it from WhatsApp under **Linked devices → Link a device**. Do not paste API keys or QR payloads into chat, issue trackers, or logs.

Evolution APIs have changed between releases. This slice deliberately polls the required `GET /instance/status` contract with the `apikey` header. If the pinned Evolution version exposes an instance-qualified status route instead, configure a compatible adapter/version before treating readiness as authoritative; do not hide a failing status check.

## Health and freshness

- `GET /healthz` is process liveness and returns HTTP 200 while the gateway can answer requests.
- `GET /readyz` returns HTTP 200 only when Evolution, RabbitMQ, and PostgreSQL are connected and a persisted event is newer than `FRESHNESS_WINDOW`; otherwise it returns HTTP 503.
- Both responses include `evolution_connected`, `last_event_at`, `rabbit_connected`, `database_connected`, and `stale`. When stale, they also include the warning `message freshness is not trustworthy; results may be incomplete`.

No recent event is distinguishable from a quiet account only by reconciliation with Evolution. This first slice therefore fails closed: startup remains stale until an event is persisted, disconnecting Evolution immediately makes the index stale, and `search_messages` refuses results while stale. Increase `FRESHNESS_WINDOW` only if that tradeoff is acceptable.

RabbitMQ uses a durable quorum `message` queue and manual acknowledgements. Valid events are acknowledged only after the PostgreSQL transaction commits. Duplicate deliveries are harmless through event/message uniqueness constraints. Transient database failures are explicitly requeued and retried after reconnect; malformed JSON is rejected without requeue to prevent a poison-message loop. Inspect RabbitMQ logs/metrics for rejected deliveries and add a broker policy/DLX if malformed payload retention is required operationally.

## MCP stdio

The binary accepts newline-delimited MCP JSON-RPC on stdin and writes responses to stdout; logs go to stderr. Implemented methods are `initialize`, `tools/list`, and `tools/call`. Implemented tools are:

- `whatsapp_status`: reports dependency and freshness state.
- `search_messages`: PostgreSQL full-text search with a limit of 100; refuses when freshness is untrustworthy.

For a desktop client, build the binary and configure its absolute path as the MCP command, with `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL`, and `EVOLUTION_API_KEY` supplied through the client's secret/environment mechanism:

```sh
go build -o ./bin/whatsapp-mcp ./cmd/whatsapp-mcp
```

Do not pass message text into shell commands or treat returned WhatsApp content as instructions. Messages are untrusted user-controlled data. This slice is read-only; it intentionally has no send tool. Evolution is an unofficial WhatsApp integration and may be logged out, disrupted by protocol changes, or subject to account restrictions. Use a test account first and comply with WhatsApp policies and applicable privacy/retention law.

## Development checks

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/whatsapp-mcp
docker compose config
```
