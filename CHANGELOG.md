# Changelog

What changed in each published version, and what an update asks of whoever runs
an instance.

Versions follow [semver](https://semver.org). While the series carries `-beta`,
the database schema can still change between versions, and that is what keeps it
from being 1.0. **There is no downgrade** — a release may migrate the schema,
migrations only run forward, and the way back is the backup `update.sh` takes
before it starts.

To update:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

## 0.2.0-beta.1

The first numbered version of the beta series. An instance can now say what it
is running, and there is one command to move it forward.

### Versioning

- The version comes from `git describe` and is stamped into the binary:
  `0.2.0-beta.1` exactly on the tag, `0.2.0-beta.1-12-gabc1234` twelve commits
  past it.
- The panel prints it in the footer, and `/healthz` and `/readyz` answer it in a
  `version` field.
- `whatsapp-mcp version` now prints the version that is serving, read from the
  container, instead of the checkout's commit.

### Updating

- `update.sh`, the command to paste into an instance: it dumps the database,
  moves to the newest release, pulls the images and waits for the gateway to
  answer. Secrets, the hostname, the pairing and the indexed messages are left
  alone.
- `whatsapp-mcp update` now calls that same script rather than having its own
  idea of what an update is — and it aims at the newest release rather than at
  the head of a branch.
- The panel says when a newer release exists, with the command ready to copy.
  The check is a public GET against GitHub every six hours, sends nothing about
  the instance, and `UPDATE_CHECK=false` stops it.

### Evolution licensing

- Activation happens without anyone opening an inbox: the registration goes to
  an address whose link is clicked by this project's own email worker.
- When that does not answer in time, the panel hands the licence back to the
  operator instead of leaving them waiting, and the activation code is spent by
  Evolution, which is the only thing that can spend it.
- First run became a wizard that walks from `/setup` to a paired WhatsApp.

### Session state

- The panel stopped announcing a dead session on top of one that was delivering
  messages: before believing Evolution's instance record, the gateway asks
  WhatsApp.
- A restart recovers the last-event time from the index instead of judging the
  session by the process's first seconds of life.

### Installation

- The installer waits for the apt lock instead of dying on it, carries the
  licence variables it was given into `.env`, and hands the operator a setup
  link so they choose their own credentials.

## 0.1.0

The first publication: a multi-architecture image on ghcr.io, Linux binaries,
and the `install.sh` that brings the whole stack up on a clean VM.
