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

## 0.5.0-beta.1

The server version catches up with WhatsApp MCP Local 1.3.0: the same 40 MCP
tools as Local (plus `send_contact` and `set_transcription_key`), webhooks, and the
same control panel.

### New tools

- **Reply, mention and draft.** `send_text_message` quotes an earlier message
  (`reply_to`), mentions people in a group (`mentions`, written in the text as
  @number) and, with `dry_run`, returns the draft and its recipient without
  sending. `send_media_message` gains `reply_to`, `dry_run` and stickers.
- **Forward, mark as read, "typing…".** `forward_message` resends a message
  marked as forwarded, media included; `mark_chat_read` clears a chat on the
  phone and sends the blue ticks; `send_typing` shows "typing…" or
  "recording audio…".
- **Who is waiting for an answer.** `list_unanswered` lists the chats whose
  latest message is from the other side, ignoring a closing "ok" or "obrigado",
  optionally with the groups that mentioned the account; `list_unread` lists
  what the phone shows as unread; `list_mentions` the messages that mention the
  account. `mark_handled` and `snooze_chat` take a chat off those lists until
  someone writes again — kept in the gateway, invisible to the other side.
- **Counting and exporting.** `message_stats` counts by chat, sender, day or
  month; `get_message_context` reads around one message; `export_messages`
  writes NDJSON and hands it over through a temporary link;
  `get_chat_messages` and `search_messages` take `fields`,
  `max_content_chars` and (reading) `count_only`.
- **Groups.** `manage_group_participants`, `update_group`,
  `get_group_invite_link` and `leave_group`. Removing someone, resetting the
  link and leaving ask for confirmation.
- **`health`**: one verdict and the checks behind it.
- **Kept files.** `download_media` keeps a copy of each file on the server's
  new data volume — it stays readable after WhatsApp discards it — and returns
  it as an image, an audio or a file block; files over 20 MiB come as a link.
  `media_stats` and `purge_media` measure and clear the volume.
- `transcribe_audio` gives Whisper the conversation as context and returns it
  with a review instruction; `save_transcript` keeps a correction's original
  as `raw_text`. Every message tool takes an optional `chat_jid`.
- Every tool carries MCP annotations (`readOnlyHint`, `destructiveHint`), and
  the server's instructions tell the assistant about webhooks.

### Webhooks

- **Configurações › Webhooks** in the panel: a script receives every new
  message, reaction or read receipt as it arrives, signed with HMAC-SHA256.
  Add, test, turn off and delete each one, and see its last delivery; a
  webhook that stops answering is turned off by itself. A documentation page
  shows each delivery's JSON. The deliveries are the same as Local's, plus
  `instance_id`. See [docs/webhooks.md](docs/webhooks.md).

### The panel

- The panel now looks and works like Local's: **Configurações** behind the
  gear (MCP address, account, webhooks, transcription, updates, appearance,
  downloaded files, data), tabs **Conectar MCP**, **WhatsApp**, **Status**,
  **Funções**, **Receitas** and **Ajuda**, a step-by-step page per AI tool
  (Claude Desktop, Claude Code, ChatGPT / Codex, Cursor, another one) that
  finishes by itself when the tool connects, connections shown with each
  tool's logo, light, dark or system theme, and a Help tab with the common
  questions and examples. The old addresses redirect.

### Ingestion

- The index reads quotes, mentions, forwards, reactions, polls and media file
  names, and applies edits and deletions to the message they change. Rows
  indexed before are filled in from their stored events on the first start.
- The gateway consumes `READ_RECEIPT` (the new `receipt` queue), which is how
  reading a chat on the phone clears it in `list_unread`.

### Updating to this version

- `update.sh` brings the new `docker-compose.yml`: Evolution restarts with
  `READ_RECEIPT` in `AMQP_GLOBAL_EVENTS`, and the gateway gets the
  `whatsapp_mcp_data` volume (`MEDIA_DIR`, `EXPORT_DIR`) and `INTERNAL_URL`.
  Migrations `014` to `017` add the webhooks, settings, triage marks, chat
  state, message details and the transcript's raw text.
- Unread counts start from the moment of the update for chats the gateway has
  no read information about; a history sync or reading the chat on the phone
  fills it in.

### Fixes

- Media WhatsApp has already discarded — its servers keep a file only for a
  while after it is sent — is now reported as that. `download_media` says the
  media expired and that only the sender resending it brings it back;
  `link: true` fetches the file before handing out a URL, so it fails there
  with the reason instead of giving a URL that answers a bare 502, and keeps
  the file for the link so the download is immediate. A large file that was
  not kept and has expired by the time it is fetched answers 410.
- The index's gap warnings sent the client to `backfill_gap`, a tool that
  does not exist. They now name `sync_history` with `before`.

## 0.4.0-beta.1

The panel can update the server it runs on, voice notes can be transcribed on
the user's own machine, and the stack fits a 1 GB server.

### Updating from the panel

- The update notice now has an **Atualizar** button. It runs the same
  `update.sh` an operator would run by SSH — database dump first — and the
  panel follows its progress until the new version answers. The gateway never
  touches Docker: the button leaves a request in a directory shared with the
  host, and a systemd unit installed by `install.sh` (and by the next
  `update.sh`) carries it out. It accepts only a version GitHub has published.
- A Dokploy deployment can use the same agent in `dokploy` mode, which moves
  `WHATSAPP_MCP_TAG` through Dokploy's API and redeploys.
- The gateway remembers the newest version that ever ran against its
  database. Starting an older one — a redeployed stale pin, an update undone —
  raises a warning on every panel page, since migrations do not run backwards,
  with a button back to that version when the agent is there.
- The gateway container now runs as uid 10001, so the host can give it the
  shared directory. Migration `013_deployed_version.sql` adds one table.

### Local transcription

- The tool descriptions now tell a client that can run commands on the
  user's machine to transcribe locally first — `mlx-whisper` on Apple
  Silicon, `faster-whisper` or `whisper.cpp` on an NVIDIA GPU — and to use
  `transcribe_audio` (OpenAI) only when that is not possible. Local is free and
  the audio never leaves the machine.
- `download_media` takes `link: true` and answers with a temporary URL and a
  `curl` command instead of the base64 file, so a voice note does not have to
  pass through the conversation. The link needs no credential, is bound to one
  message and expires after ten minutes.
- `save_transcript` stores a transcript made elsewhere. `get_chat_messages`
  and `search_messages` now return every kept transcript in the message's
  `transcript` field, and search matches words spoken in voice notes.

### Smaller machines, and AWS

- The stack runs on 1 GB of RAM. On a machine with less than 2 GB and no swap,
  the installer adds a 2 GB swap file, which is what gets a 1 GB plan through
  the image pulls and the first history sync. A 1 GB AWS Lightsail instance ran
  the whole stack at about 520 MB used, with nothing killed.
- [docs/aws.md](docs/aws.md) and [docs/aws.pt-BR.md](docs/aws.pt-BR.md) walk
  through Lightsail: the US$ 7 plan, why the price is the same in every
  region, pinning a static IP before installing, opening 443, the
  builder.aws.com projects whose region is fixed, and the AWS CLI commands an
  agent such as Claude Code can run while the user only signs in. The
  README's install prompt offers that route.

### Fixes

- A fresh install failed at "starting the stack": `quay.io/minio/minio`
  started answering 401, after MinIO archived its community edition and
  stopped serving it anywhere. MinIO is gone from the stack instead of being
  pulled from somewhere else — it never held anything. Evolution only writes
  media to it with `WEBHOOK_FILES` on, which this stack leaves off, and no
  bucket was ever created; the gateway fetches media from WhatsApp through
  `/message/downloadmedia`. An existing instance loses the idle container on
  its next update, and the empty `whatsapp-mcp_minio_data` volume can be
  removed with `docker volume rm whatsapp-mcp_minio_data`. The `MINIO_*`
  lines left in its `.env` are ignored.
- Voice notes that arrived through a history sync could not be downloaded or
  transcribed ("the stored payload carries no media"). The media lookup only
  understood live events; a history sync stores whole conversations, and the
  message is now found among them by its id.
- A conversation WhatsApp moved to a LID (`…@lid`) was split in two: the
  history under the phone number, the newest messages under the LID. The
  gateway now records which LID belongs to which number — from the
  `RecipientAlt`/`SenderAlt` of live messages and the `pnJID` of history syncs
  — and `list_chats` shows one conversation, `get_chat_messages` reads both
  JIDs whichever one is asked for. Messages keep the JID WhatsApp used, so
  deleting, editing and reacting still address them correctly. Migration
  `012_jid_aliases.sql` pairs what is already indexed; LID chats whose number
  WhatsApp never revealed stay on their own.

## 0.3.0-beta.2

### Fixes

- `transcribe_audio` failed on every voice note with "Evolution returned no
  audio". Evolution Go returns the media as a data URI
  (`data:audio/ogg; codecs=opus;base64,…`) with an empty `mimetype`, and the
  tool was decoding it as bare base64. It now reads the format from the URI and
  falls back to Ogg/Opus, which is what WhatsApp voice notes are.

## 0.3.0-beta.1

Voice notes can now be read. An instance that saves an OpenAI key gets its
WhatsApp audio transcribed on request.

### Voice-note transcription

- `transcribe_audio` turns a WhatsApp voice note into text with OpenAI's
  Whisper. An AI client cannot hear the audio `download_media` returns, so the
  gateway does this one piece of model work itself — only with an OpenAI key the
  operator saves, and billed to that key.
- The key is saved from the panel's new **Transcrição** page or with the
  `set_transcription_key` tool. Both check it with OpenAI before saving, and
  neither shows it again: only its last four characters.
- The **Transcrição** page walks someone who has never used the OpenAI
  platform through getting a key — account, credit, key — with a button to
  each OpenAI page and what it will cost. Once a key is saved, it links to
  OpenAI's usage, credit, spending-limit and key pages instead.
- When there is no key, or OpenAI refuses it, `transcribe_audio` answers with
  the same steps and links, so the AI client can guide the user through setup
  instead of reporting an error.
- Every transcript is kept, so asking again for the same voice note costs
  nothing. `whatsapp_status` reports whether transcription is configured.
- The update adds two tables (migration `011_transcription.sql`); nothing
  existing changes. The gateway needs outbound HTTPS to `api.openai.com` for
  transcription to work.

### Publishing

- The `latest` image tag and the GitHub release marked Latest now follow the
  newest version, beta included. Pinning nothing used to leave an instance on
  0.1.0 — dozens of commits old, under the previous licence — while the beta
  was the version to run.

## 0.2.0-beta.1

The first numbered version of the beta series. An instance can now say what it
is running, and there is one command to move it forward.

### Licence

- The project is now under the **MIT licence**, replacing PolyForm
  Noncommercial. Use it, modify it, fork it, distribute it and sell it, keeping
  the copyright notice. The previous licence was a barrier for exactly the
  people this gateway is for — a small business running support or sales on the
  WhatsApp number it connects.

### The panel

- The Conectar page was rebuilt around connections rather than keys. It answers
  "is everything working" with the WhatsApp number and how many AI tools are
  attached to it, and one button adds another: it asks where the connection
  will be used, mints the key without being asked, and opens the instructions
  for that client.
- Each connection is listed by the name its own tool gave in the MCP handshake
  — "Claude Desktop", not a key prefix. Every MCP client names and versions
  itself once, at `initialize`; that is read, stored against the key, and
  refreshed on each later handshake, so a credential moved to another tool
  corrects itself. The name is a claim and never a proof: it is never consulted
  for authorisation.
- Instâncias and Estado stay as technical as they were — they are for a
  different moment.

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
