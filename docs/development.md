# Local development

`just` drives everything. `just` on its own lists the recipes.

There are three ways to run it, from least to most setup:

```sh
just preview          # the panel alone, fake data, no dependencies at all
just tunnel           # in one terminal: SSH tunnel to a remote Evolution
just dev-remote       # in another: the gateway against that Evolution
just up && just dev   # the whole stack locally, with a WhatsApp of your own to pair
```

**`just preview`** serves the control panel against fabricated data on port
8090. It talks to nothing, so it is the fastest way to work on layout and
wording.

**`just dev-remote`** runs the gateway locally against an Evolution running
elsewhere. Live reads and sending work; ingestion does not, and that is expected
— the remote Evolution publishes to the remote queue, not to yours, so the local
index stays empty. Evolution has no published port, so `just tunnel` asks the
host for the container's address on the Docker bridge and forwards to it. Set
`TUNNEL_HOST` in your `.env` to point it at your own server.

**`just up && just dev`** runs everything locally and needs a WhatsApp account to
pair. It is the only mode where ingestion, history sync and the message index
actually work.

## Before a commit

```sh
just check   # format, vet, tests, race, build, compose validation
```

or by hand:

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/whatsapp-mcp
docker compose config
```

## Images

`ghcr.io/brorlandi/whatsapp-mcp` is built and pushed by
`.github/workflows/release.yml`: `edge` on every push to `main`, and the version
tags plus the Linux binaries on every `v*` tag. The Dockerfile cross-compiles
from the runner's own architecture rather than emulating the target, so the
arm64 image costs about as much as the amd64 one.

`docker compose up -d` pulls that image. `docker compose up -d --build`
compiles the working tree instead — use it whenever you are testing a change
to the gateway itself through Compose.

## Releasing

The version is the git tag, and nothing else. There is no `VERSION` file to
forget to bump: the binary carries `git describe --tags --always --dirty`,
stamped through `-ldflags` by the Dockerfile, by `release.yml` and by
`just build`. `just version` prints what this checkout would compile as.

To publish one:

```sh
just release 0.2.0-beta.2      # writes the tag, after checking CHANGELOG.md
git push origin v0.2.0-beta.2  # this is what triggers release.yml
```

Two rules the workflow encodes, both worth knowing before tagging:

- **A prerelease must not move `latest`.** `latest` is what someone gets when
  they pin nothing, so a `-beta` tag would otherwise make the beta the default
  for everybody. The `flavor: latest=` expression excludes any tag with a
  hyphen in it, which is the semver definition of a prerelease.
- **The checkout must be deep.** `actions/checkout` is shallow by default and
  `git describe` then sees no tags at all, silently stamping a bare sha.

Write the `CHANGELOG.md` section before tagging. It is what turns a number into
information: without it, "0.3.0" tells nobody whether it is worth updating to,
and the panel links straight at it from the update notice.

There is no downgrade. A release may migrate the schema, migrations only run
forward, and [updating.md](updating.md) is where that is spelled out for the
people running instances.

## Transports

`POST /mcp` is the supported transport. A stdio transport exists for debugging a
local build and is off unless `MCP_STDIO=true`; it carries no credential, so it
acts on the instance the panel selected. Do not enable it on a deployed
instance.

Running the binary directly needs `DATABASE_URL`, `RABBITMQ_URL`,
`EVOLUTION_URL` and `EVOLUTION_API_KEY`.

## Brand assets

`internal/brand/` holds the visual identity and every file there is embedded
into the binary, so the panel never reaches for a CDN and the content security
policy can stay at `'self'`.

- `logo.svg` — the mark used in the masthead and in the README.
- `favicon.svg` — the same mark simplified into a filled tile, because the open
  outline turns to mush at 16 px.
- `favicon.ico` (16/32/48) and `apple-touch-icon.png` (180) are **generated**
  from `favicon.svg`. Regenerate them rather than editing them:

  ```sh
  for n in 16 32 48; do rsvg-convert -w $n -h $n internal/brand/favicon.svg -o /tmp/ico$n.png; done
  rsvg-convert -w 180 -h 180 internal/brand/favicon.svg -o internal/brand/apple-touch-icon.png
  python3 - <<'PY'
  import struct
  sizes = [16, 32, 48]
  imgs = [open(f"/tmp/ico{n}.png", "rb").read() for n in sizes]
  out = struct.pack("<HHH", 0, 1, len(sizes))
  off = 6 + 16 * len(sizes)
  for n, d in zip(sizes, imgs):
      out += struct.pack("<BBBBHHII", n, n, 0, 0, 1, 32, len(d), off)
      off += len(d)
  open("internal/brand/favicon.ico", "wb").write(out + b"".join(imgs))
  PY
  ```

## Shell safety

Do not pass message text into shell commands. It is third-party content and
nothing guarantees it is not a quoting attack.
