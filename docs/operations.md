# Operations

## Health and freshness

- `GET /healthz` is process liveness and returns HTTP 200 while the gateway can
  answer requests.
- `GET /readyz` returns HTTP 200 only when Evolution, RabbitMQ and PostgreSQL
  are connected and a persisted event is newer than `FRESHNESS_WINDOW`;
  otherwise HTTP 503.
- Both carry the whole picture: the WhatsApp session state with the reason
  WhatsApp gave, per-queue consumption with counters, and the problems in plain
  language. The same snapshot feeds the panel and the `whatsapp_status` tool, so
  all three describe a failure identically.
- Authenticated `GET /api/selected-instance` reports the persisted selection and
  one of `api_unavailable`, `no_instance`, `disconnected`, `connecting` or
  `connected`.

A quiet account and a broken pipeline look alike from age alone, so the tools
report rather than refuse: a degraded gateway still answers, with the problems
and the index coverage attached. Refusing would hide the messages that *are*
indexed and leave the caller unable to tell which situation they are in.

Session state comes from Evolution's connection events. The readiness poll is
reconciliation after a restart, and it may confirm a live session but never
overwrite a specific failure with a vague one: `logged_out` tells the operator
to scan a new QR code, `disconnected` tells them to wait.

## The event pipeline

RabbitMQ uses durable quorum queues and manual acknowledgements. The gateway
consumes every queue the subscribed events create — `message`, `sendmessage`,
`historysync` and the six connection queues — because a queue Evolution declares
and nobody reads grows without bound.

- Valid events are acknowledged only after the PostgreSQL transaction commits.
- Duplicate deliveries are harmless through event and message uniqueness
  constraints.
- Transient database failures are explicitly requeued and retried after
  reconnect.
- Malformed JSON is rejected without requeue, to prevent a poison-message loop.

Inspect RabbitMQ logs and metrics for rejected deliveries, and add a broker
policy or a dead-letter exchange if retaining malformed payloads matters
operationally.

`RABBITMQ_QUEUES` narrows the consumed set, as a comma-separated list. Leave it
unset unless you have a reason: a declared queue nobody drains fills the disk.

## Configuration

Read by the `whatsapp-mcp` binary:

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Address the HTTP server binds to inside the container. |
| `PUBLIC_URL` | `http://127.0.0.1:8080` | Address clients reach the gateway at. Printed in the generated client configuration. Not a secret. |
| `DATABASE_URL` | — | PostgreSQL connection string for the index. Required. |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | Broker connection string. |
| `RABBITMQ_QUEUES` | every subscribed queue | Comma-separated override, to narrow the consumed set. |
| `EVOLUTION_URL` | `http://evolution-go:4000` | Evolution Go's internal address. |
| `EVOLUTION_API_KEY` | — | Evolution's global key, for administrative routes only. Required. |
| `EVOLUTION_TIMEOUT` | `5s` | Per-request timeout against Evolution. |
| `FRESHNESS_WINDOW` | `5m` | How old the newest persisted event may be before `/readyz` reports 503. |
| `STATUS_POLL_INTERVAL` | `15s` | How often the gateway reconciles session state with Evolution. |
| `MCP_STDIO` | unset | `true` enables the stdio transport. Development only — it carries no credential. |

The Compose stack adds the credentials for the services it runs
(`EVOLUTION_DB_*`, `RABBITMQ_*`, `MINIO_*`, `MCP_DB_*`) plus `PANEL_BIND` and
`PANEL_PORT`. See `.env.example`.

None of these ever reach an MCP client. They are backend configuration.

## Retention

The database grows without limit. Cleanup is deliberately deferred until there
is real volume to size a ceiling against, and the rule is already fixed: delete
messages, never conversations.

Message text and raw event payloads are stored unencrypted in PostgreSQL. A
database dump is as sensitive as the phone it came from. See issue #4.

## Troubleshooting

| Symptom | Where to look |
|---|---|
| `/readyz` 503 right after install | Expected until an instance is paired and an event has arrived. |
| Evolution reported unavailable | Evolution Go has not been activated, or its container is not healthy. |
| Panel shows an instance it refuses to operate | The instance was created outside the panel, so there is no stored token for it. |
| Messages stop being indexed | Check the per-queue counters in `/readyz` or `whatsapp_status`; an event queue that stops advancing points at the consumer, one that never fills points at the subscription. |
| Reads return an empty period | Check the coverage in the same snapshot before concluding nothing was said. |
