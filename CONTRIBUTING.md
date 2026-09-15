# Contributing

Bug reports, questions and pull requests are all welcome.

## Before you open a pull request

Run what CI would run:

```sh
just check   # gofmt, go vet, go test, -race, go build, docker compose config
```

or by hand:

```sh
gofmt -w cmd internal
go vet ./...
go test ./...
go build ./cmd/whatsapp-mcp
docker compose config
```

[docs/development.md](docs/development.md) explains the three ways to run the
project locally. `just preview` needs no dependencies at all and is the fastest
way to work on the panel.

## What makes a change easy to merge

- **One thing at a time.** A change that fixes a bug and renames three files is
  two pull requests.
- **A test for behaviour that could regress.** The existing tests are a good
  guide to the level of detail expected.
- **Comments that say why, not what.** The codebase explains the reasoning
  behind decisions that look odd; please keep that up rather than describing
  what the line already says.
- **The panel is in Portuguese.** The code, comments and documentation are in
  English. Keep that split.

## Reporting a bug

Say what you did, what happened and what you expected. For anything involving a
running installation, `whatsapp_status` or `GET /readyz` returns the whole
picture — session state, queue counters, index coverage and the problems in
plain language. Paste that.

**Never paste an API key, a QR payload, message content or a `.env`** into an
issue. If the report needs them, say so and they can be handled privately.

For a security vulnerability, do not open an issue — see [SECURITY.md](SECURITY.md).

## Licence and the DCO

This project is released under the [PolyForm Noncommercial
1.0.0](LICENSE) licence, which is source-available rather than open source in
the OSI sense: free for any noncommercial use, and commercial use needs a
separate licence from the author.

By contributing you certify the [Developer Certificate of
Origin](https://developercertificate.org/) and you agree that your contribution
is licensed to the project's author under terms that allow relicensing —
including under a different licence in the future. That last part is not
boilerplate: without it, a single merged patch would permanently freeze the
licence, and the ability to open the project up later would be gone.

Sign off your commits:

```sh
git commit -s -m "fix: ..."
```
