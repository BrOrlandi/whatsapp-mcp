# Evolution Go licensing, and what this panel automates of it

The one step in this project's installation that no code can finish on its own
is the first registration of an operator's email with Evolution Foundation's
licensing server — because that registration *is* the proof that the email is
theirs. This explains what the licence is, what the licensing server exposes,
how far this project drives it without a browser, and the one click that is
left and why it stays.

Everything below was read from
[`evolution-foundation/evolution-go`](https://github.com/evolution-foundation/evolution-go)
at tag `0.7.2` (`9337afc`), in `pkg/core/c0.go`, and — where it says so —
probed against the live licensing server.

## What the licence gates

Evolution Go refuses to serve until it is activated. Its own startup says so:

```
API endpoints will return 503 until license is activated.
Use GET /license/register to get the registration URL.
```

Three routes stay open while everything else answers 503 — `/license/status`,
`/license/register` and `/license/activate` (`pkg/core/c0.go:643`). That is how
this project's panel can report the real reason instead of "the API is
unavailable, try again shortly", which would be advice to wait for something
that is never going to happen on its own.

## Where the licence lives

Not in a file. It goes into Evolution's own PostgreSQL, in the table
`runtime_configs` — a key/value table declared at `pkg/core/c0.go:147` with
`TableName()` returning `runtime_configs` at line 155.

`_yosh` (`pkg/core/c0.go:233`) writes three rows through `_yy`
(`pkg/core/c0.go:191`), an upsert on `key`:

| Key | What it is |
|---|---|
| `api_key` | The licence credential issued by the licensing server |
| `tier` | The plan the credential was issued under |
| `customer_id` | The account it belongs to |

`instance_id` lives in the same table and identifies this installation.

In this project's Compose stack that database is the `postgres-evolution`
service, so the activation survives a container rebuild as long as the
`postgres_evolution_data` volume survives. Losing that volume means activating
again — which, since the panel keeps the credential, nobody has to notice.

## How activation happens, end to end

The licensing server is `https://license.evolutionfoundation.com.br`, assembled
one fragment at a time in `_cdo()` (`pkg/core/c0.go:35`) rather than written
as a constant. All calls go through `_dtnx` (`pkg/core/c0.go:114`).

The whole registration is four moves, and only one of them needs a person:

1. **`POST /v1/register/init`** with `{tier, version, instance_id}` — an
   Evolution route (`GET /license/register`) calls this, and accepts a
   `redirect_uri` for where the operator should land afterwards. It returns a
   registration token bound to this `instance_id`.
2. **`POST /v1/auth/magic-link`** with `{token, email, name}` — this is all
   the register *page* does when a person opens it; a server can make the
   same call with no browser involved. The licensing server emails the
   operator a link valid for 15 minutes. *(Probed live: `{"status":"sent"}`.)*
3. **The click.** The operator clicks the link in their own inbox. This is
   the identity proof — the thing being registered is precisely the ownership
   of that email — so no automation can honestly skip it. The licensing
   server then redirects the browser to the `redirect_uri` with a one-time
   authorization `?code=`.
4. **`POST /v1/register/exchange`** with `{authorization_code, instance_id}`**
   returns the `{api_key, tier, customer_id}`. Evolution's
   `GET /license/activate?code=…` route does this and stores the result; the
   exchange can equally be made by anyone holding the code, and the key it
   returns also works directly as the `code` for `/license/activate`
   (`_58` at `pkg/core/c0.go:591` falls back from code to key). *(Probed
   live: a wrong code answers `401 AUTH_CODE_EXPIRED`.)*

Evolution's own Manager UI walks these same moves; the difference here is who
drives them.

## The headless path Evolution ships, and its current state

Evolution Go also documents a no-browser activation: with
`EVOLUTION_OPERATOR_EMAIL` set, `_rh` (`pkg/core/c0.go:503`) posts to
`/v1/register/auto` on startup with `{email, tier, version, instance_id}`, and
a **previously registered** email gets an `api_key` back. An unknown email
gets a 404 and the process falls back to the manual flow
(`pkg/core/c0.go:526`). It was added in this project in `3b03c94`.

As of this writing, though, the **live** licensing server answers
`/v1/register/auto` with `401 {"code":"UNAUTHORIZED","error":"missing token"}`
without an `Authorization: Bearer …` header — and the register token from
`/v1/register/init` is rejected as `invalid token` there. Evolution Go 0.7.2
sends no such header (checked against tag `0.7.2` and `main`, which are
identical). So the shipped headless path may be dead against the current
server; the panel's flow below does not depend on it at all. It is kept in
`.env` as a fallback that costs nothing if the server accepts it again.

## Why the first registration still needs a person

There is no input an installer could invent that would produce a licence. The
credential is issued by a server this project does not control, in exchange
for an identity the operator has to establish once — by clicking a link in the
inbox they own.

That step is not incidental. Evolution Go is Apache-2.0 **with additional
brand-protection conditions, including a Usage Notification requirement**, and
the registration is how that notification happens. It is a condition of the
grant under which this project is allowed to depend on and redistribute
Evolution Go at all.

## Why forging one is not an option

It would also not work. Activation is not a local boolean:

- The `api_key` is issued by the licensing server and stored; it is not computed
  from anything available locally.
- `ActivateIntegrity` (`pkg/core/c0.go:364`) derives a checksum from the key and
  the instance id.
- A heartbeat runs every 30 minutes — `hbInterval` at `pkg/core/c0.go:373`,
  posting to `/v1/heartbeat` at line 945 — so a forged local state is checked
  against the server repeatedly, not once at startup.

Beyond that, sharing one registration across every install of a public project
would defeat the Usage Notification condition deliberately, and put the person
whose account was used at the centre of it: their licence covering strangers'
deployments, and a revocation breaking every installation at once.

## What this project does instead

The panel drives the whole registration itself, from the first step of the
installation wizard at `/instalacao` (`internal/evolution/licensing.go`,
`internal/httpapi/web.go`). There are two modes, chosen by
`EVOLUTION_LICENSE_AUTO`:

**Automatic (the default, `EVOLUTION_LICENSE_AUTO=true`).** The wizard
registers the licence with an address of this deployment's own —
`whatsappmcp+<random>@EVOLUTION_LICENSE_EMAIL_DOMAIN` (default
`brorlandi.xyz`); a fresh plus-addressed `whatsappmcp+…` per installation, so
every deploy is its own registration in the licensing server's books. That
address is chosen for how Cloudflare Email Routing routes it: one exact rule
for the `whatsappmcp` base — with subaddressing enabled — matches every
`whatsappmcp+<detail>` and nothing else, so no catch-all faces the worker and
the domain's personal mail never passes through it. The magic-link email for
those addresses is delivered to
[whatsapp-mcp-license-worker](https://github.com/BrOrlandi/whatsapp-mcp-license-worker)
(private), an Email Worker that finds the link in the message and does the GET
a browser would: the same click, server-side. The licensing server redirects
to the panel's activation callback, the wizard's poll notices the step change,
and the operator typed nothing, opened no inbox, clicked nothing.

**Manual (`EVOLUTION_LICENSE_AUTO=false`).** The operator confirms the email
typed at setup and clicks the emailed link themself — the flow this
repository had before the worker: same wizard, one click, once per email,
ever. (A URL had to be built by hand rather than `url.JoinPath`, which
escapes a `?` written into a path segment.)

Either way, the callback exchanges the one-time code for the `api_key`,
activates Evolution with it, and keeps a copy in the panel's own database
(`evolution_license`, migration `008`). **From then on, nobody is asked
anything.** If a rebuild loses the Evolution volume, the panel notices the 503,
hands Evolution the key it kept, and carries on; a rebuild that loses both
volumes is a new registration, and even then the wizard does it again by
itself in automatic mode.

The callback accepts the code without a panel session: the code is a
single-use, short-lived capability the licensing server issued exactly like the
installer's setup token, and it stops meaning anything the moment it is spent.
`EVOLUTION_OPERATOR_EMAIL` in the Compose stack stays as a belt-and-braces
startup fallback, documented above.

## If that is still too much

The way to remove the click is to ask, not to route around it. Evolution
Foundation lists a contact for licensing enquiries, and an open-source
installer that puts Evolution Go on third-party servers is a distribution
channel rather than lost revenue. What to ask for is a distribution
identifier or a non-interactive per-installation activation: every deployment
still counted, which is what the Usage Notification is for, without a form
in the middle.

The alternative that removes the dependency entirely is talking to
[whatsmeow](https://github.com/tulir/whatsmeow) directly — the MIT-licensed
library Evolution Go itself uses, as its own CHANGELOG notes ("Dropped the
whatsmeow fork — now uses official `go.mau.fi/whatsmeow`"). That would also
remove RabbitMQ and MinIO from the stack, at the cost of rewriting the session
layer.
