# Evolution Go licensing, and why the install cannot be fully unattended

The one step in this project's installation that a script cannot finish on its
own is activating Evolution Go. This explains what that step is, why it needs a
person the first time, and how far it has been automated.

Everything below was read from
[`evolution-foundation/evolution-go`](https://github.com/evolution-foundation/evolution-go)
at tag `0.7.2` (`9337afc`), in `pkg/core/c0.go`, and confirmed against a running
container.

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
again.

## How activation actually happens

The licensing server is `https://license.evolutionfoundation.com.br`, assembled
one fragment at a time in `_cdo()` (`pkg/core/c0.go:35`) rather than written as
a constant. All calls go through `_dtnx` (`pkg/core/c0.go:114`).

There are two paths to a licence.

**Browser registration.** `GET /license/register` returns a URL carrying a token
bound to this `instance_id`:

```json
{
  "register_url": "https://license.evolutionfoundation.com.br/register?token=…",
  "status": "pending"
}
```

The operator opens it, registers with an email, and the server activates that
instance. The instance is live immediately — no restart needed. Internally this
is the `/v1/register/init` → `/v1/register/exchange` pair
(`pkg/core/c0.go:730`, `786`).

**Headless activation.** With `EVOLUTION_OPERATOR_EMAIL` set, `_rh`
(`pkg/core/c0.go:503`) posts to `/v1/register/auto` on startup:

```json
{"email": "…", "tier": "…", "version": "…", "instance_id": "…"}
```

A registered email gets an `api_key` back, which is stored through `_yosh` and
activates the process. This is the documented feature — `.env.example` line 10
and the CHANGELOG entry "Headless license auto-activation".

## Why the first activation still needs a person

`/v1/register/auto` **recognises an email, it does not create one.** An address
that has never registered gets a 404, and `_rh` handles it explicitly
(`pkg/core/c0.go:526`):

```
ℹ Auto-activation skipped — email not registered yet (first time?).
  Falling back to manual flow.
```

So there is no input an installer could invent that would produce a licence.
The credential is issued by a server that this project does not control, in
exchange for an identity the operator has to establish once.

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

1. The panel detects the 503 and says the licence is missing rather than
   reporting a transient outage, and shows the registration link read from
   Evolution's own `/license/register`.
2. `EVOLUTION_OPERATOR_EMAIL` is passed through by the Compose stack and can be
   given to `install.sh` in its environment, so every machine after the first —
   and every rebuild that loses the Evolution volume — comes up activated with
   no browser step.
3. The operator registers once, ever, under their own identity.

The residual cost is one browser visit per operator, not per server.

## If that is still too much

The way to remove it is to ask, not to route around it. Evolution Foundation
lists a contact for licensing enquiries, and an open-source installer that puts
Evolution Go on third-party servers is a distribution channel rather than lost
revenue. What to ask for is a distribution identifier or a non-interactive
per-installation activation: every deployment still counted, which is what the
Usage Notification is for, without a form in the middle.

The alternative that removes the dependency entirely is talking to
[whatsmeow](https://github.com/tulir/whatsmeow) directly — the MIT-licensed
library Evolution Go itself uses, as its own CHANGELOG notes ("Dropped the
whatsmeow fork — now uses official `go.mau.fi/whatsmeow`"). That would also
remove RabbitMQ and MinIO from the stack, at the cost of rewriting the session
layer.
