# Evolution Go, patched

The stack runs `ghcr.io/brorlandi/whatsapp-mcp:evolution-0.7.2-patch.1` instead of the
official `evoapicloud/evolution-go:0.7.2`. It is the official 0.7.2 source with the two
commits of [evolution-foundation/evolution-go#190](https://github.com/evolution-foundation/evolution-go/pull/190)
applied, built by [`.github/workflows/evolution.yml`](../../.github/workflows/evolution.yml).
It is an unofficial build, not endorsed by Evolution Foundation.

## Why

Evolution Go 0.7.2 drops its WhatsApp connection about every 50 minutes
([issue #6](https://github.com/BrOrlandi/whatsapp-mcp/issues/6),
[evolution-go#211](https://github.com/evolution-foundation/evolution-go/issues/211),
[evolution-go#185](https://github.com/evolution-foundation/evolution-go/issues/185)):

```
[Client ERROR] Unknown stream error: <stream:error><ack class="status" id="…" type="media"/></stream:error>
Disconnected detected, restarting instance
```

- The whatsmeow it pins (2026-06-30) loses the acknowledgement of a media Status across
  a reconnect. WhatsApp keeps waiting for it, ends the stream about every 50 minutes,
  and delivers the same Status again.
- Every reconnect opens a new Postgres pool (`sqlstore.New` in `StartClient`) that is
  never closed. After about a hundred cycles Evolution's Postgres runs out of
  connections, Evolution can no longer load the session, and nothing is ingested.

## What the patches change

| Patch | Change |
|---|---|
| `0001` | whatsmeow to `v0.0.0-20260904121843-28bfe537ea6a` ("ensure stream error is handled before reconnecting", "don't reuse handler queue between connections"), Go 1.26 in the Dockerfile, and `SetStatusMessage`'s new argument type. |
| `0002` | One whatsmeow store container per DSN, reused across reconnects, instead of a new one per connection. |

Both are the commits of PR #190, by its author, unchanged. The user interface (the
Evolution manager) is not modified.

## Building it locally

```bash
deploy/evolution/prepare.sh /tmp/evolution-go
docker build --build-arg VERSION=0.7.2 -t evolution-patched /tmp/evolution-go
```

## Changing it

Change the files in `patches/` and raise the patch number in `TAG` in the workflow and
in `docker-compose.yml`. Pushing to `main` builds and publishes it.

## When to drop it

Once Evolution Go publishes a release with these fixes: point `docker-compose.yml` back
at `evoapicloud/evolution-go`, delete this directory and the workflow, and close issue #6.

## Licence

Evolution Go is Apache-2.0 with the conditions in its
[LICENSE](https://github.com/evolution-foundation/evolution-go/blob/main/LICENSE) and its
[trademark policy](https://github.com/evolution-foundation/evolution-go/blob/main/TRADEMARKS.md).
The patches are the modifications Apache-2.0 asks a redistribution to state.
