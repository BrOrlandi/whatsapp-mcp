# Updating

An installation made by `install.sh` updates with one command. It moves the
code and the images to the newest release and touches nothing else: the
secrets, the hostname, the WhatsApp pairing and the indexed messages stay where
they are.

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

On a machine that already has the command, `sudo whatsapp-mcp update` does the
same thing — it is the same script, reached by another route.

## What it does, in order

1. **Finds the installation** at `/opt/whatsapp-mcp` (or at `INSTALL_DIR`) and
   refuses to continue if there is none. Updating is not installing: on an
   empty machine the command is `install.sh`.
2. **Reads the running version** by asking the container, not the checkout —
   the two diverge whenever an image was pulled without the source moving.
3. **Dumps the gateway's database** with `pg_dump`, before anything starts.
   This is the step that makes the update reversible, and it is mandatory: if
   the dump fails, the update stops there.
4. **Moves the checkout** to the newest release — the last tag reachable from
   the branch's history. `REPO_REF` pins something else: a specific tag, or
   `main` to follow the edge build.
5. **Pulls and restarts** with `docker compose up -d --pull always`, with the
   image tag aligned to the release it just checked out. Migrations run when
   the gateway starts.
6. **Reinstalls `/usr/local/bin/whatsapp-mcp`**, which changes between versions
   like anything else.
7. **Waits for `/healthz`.** If it never answers, it prints the last lines of
   the log and says how to go back.

## It never moves backwards

`install.sh` installs the head of `main`, which runs ahead of the newest release
for as long as `main` has commits that release does not. Aiming an update at
that release from there would be a downgrade wearing the word "update" — with
migrations already applied, and no way back but the dump. So the script checks:
when the release it would move to is already an ancestor of what is installed,
it says so and stops.

An explicit `REPO_REF` is different — that is someone naming what they want, and
they get it, including a version older than what is running. The dump is taken
either way.

## There is no downgrade

A release may migrate the schema, and migrations only run forward. Putting the
old code back over a migrated schema breaks the instance in a second way.

The way back is the dump from step 3, and restoring it discards every message
received since it was taken. That is why the script does not roll back on its
own: it is a decision with data loss in it, and it belongs to whoever operates
the instance. When an update fails, the script prints the exact commands and
stops.

Dumps live in `/opt/whatsapp-mcp/backups/`, the five most recent of them
(`BACKUP_KEEP` changes the number). **They contain message text** — treat them
exactly as you would treat the phone.

## Variables

| Variable | What it does |
|---|---|
| `INSTALL_DIR` | Where the installation is. Default `/opt/whatsapp-mcp`. |
| `REPO_REF` | What to move to: a tag (`v0.2.0-beta.1`), or `main` for the edge build. Default: the newest release. |
| `BACKUP_KEEP` | How many dumps to keep. Default 5. |
| `FORCE` | `true` re-runs even when already on the target version. |

## The notice in the panel

The gateway asks GitHub every six hours whether a newer release exists, and
when one does the panel shows the notice with this same command ready to copy.
The question is a public GET, carries nothing about the instance, and fails
silently on a server with no outbound network.

`UPDATE_CHECK=false` in `.env` stops the check. The notice only appears on the
pages behind the session cookie: telling an anonymous visitor that this
instance is running an outdated version is an invitation.

## Which version is running

```sh
whatsapp-mcp version
curl -s https://your.host/healthz | jq -r .version
```

Both answer with what the container reports. `0.2.0-beta.1` is a published
release exactly on its tag; `0.2.0-beta.1-12-gabc1234` is a build twelve
commits past it, which is what the `edge` image is.

## Through Claude Code, over SSH

The command above handles the update. What it cannot do is diagnose: when
something breaks, somebody has to read a log. This prompt is for that — paste
it into a Claude Code with SSH access to the instance.

It is deliberately conservative. It does not improvise the update: it runs the
script, and investigates only if that fails. Restoring a backup it **proposes**
and never performs — an agent with root on somebody's production instance is
exactly where a destructive action asks first.

```
Update my WhatsApp MCP instance, which runs at <user>@<host>.

Rules:
- The update is the project's script. Do not improvise its steps, do not edit
  docker-compose.yml, .env or any file in the checkout, and do not run docker
  commands outside the script.
- Never restore a backup, delete a volume, run `docker compose down -v`, or any
  other command that loses data. If you believe one is needed, tell me which
  command and why, and wait for my answer.

Steps:
1. Connect over SSH and show the current state: `sudo whatsapp-mcp version` and
   `sudo whatsapp-mcp status`.
2. Run the update:
   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
3. If it succeeds, confirm with `sudo whatsapp-mcp version` and tell me which
   version it went from and to.
4. If it fails, do not try to fix it yourself. Collect and bring me:
   - the last 100 lines of `sudo whatsapp-mcp logs whatsapp-mcp`
   - `sudo whatsapp-mcp status`
   - the path of the backup the script said it took
   Then tell me what you think broke and what you would do next, without doing it.
```

Replace `<user>@<host>` with your instance's SSH access. The panel prints the
URL; the SSH host is the same machine.
