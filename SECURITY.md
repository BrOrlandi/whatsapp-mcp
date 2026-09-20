# Security

This gateway holds a live WhatsApp session and an unencrypted copy of the
messages it has ingested. Treat an instance of it as you would treat the phone.

## Reporting a vulnerability

Report privately through [GitHub's security advisory
form](https://github.com/BrOrlandi/whatsapp-mcp/security/advisories/new). Please
do not open a public issue for anything that affects a running installation.

Include what you did, what happened, and what you expected. A proof of concept
helps; a working exploit posted publicly does not.

## What is defended

| Surface | How |
|---|---|
| The MCP endpoint | An API key in the `Authorization` header, never a query string. `wamcp-` plus 24 random alphanumerics, ~142 bits, stored only as a SHA-256 digest and compared in constant time. No anonymous mode. Failures are throttled per source address and revocation is immediate, because each request is authorised on its own. |
| The panel | A single administrator, bcrypt-hashed. Signed HttpOnly `SameSite=Strict` session cookies with server-side state, `Secure` whenever the request arrived over TLS. Failed sign-ins are throttled per source address. A process restart invalidates every session. |
| The first-run form | Guarded by `SETUP_TOKEN` when set: the form opens only at `/setup?token=<value>`, answers 403 otherwise, and is throttled per source address. The installer generates the token and prints it inside the link. It closes the window in which an address exists with no administrator and belongs to whoever asks first. |
| Instance isolation | A key is bound to one instance. Every index read is scoped by that instance id and every live call carries that instance's own Evolution token, so a key cannot reach another instance's conversations. |
| The internal network | Only 80 and 443 are published, and 80 only redirects and answers the ACME challenge. Evolution Go, RabbitMQ, MinIO and both PostgreSQL instances stay on the Compose network; the RabbitMQ and MinIO management ports bind to `127.0.0.1`. |
| Evolution credentials | The per-instance token is minted and stored by the panel, never displayed and never returned by a tool. The global Evolution key is used only for administrative routes. |
| Media URLs | `send_media_message` refuses a URL that points back into the deployment — private addresses, loopback, container names, and any scheme other than http(s) — because Evolution fetches that URL from inside the Docker network. |
| The browser | `default-src 'self'`, `frame-ancestors 'none'`, `form-action 'self'`, `base-uri 'none'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. No CDN: every asset is embedded in the binary. |
| Prompt injection | Message content is written by third parties. Every reading tool labels it as data rather than instructions, and destructive tools refuse messages this account did not send. `delete_message` will not act without a separate confirmed call. |

## What is not defended, and you should know it

- **Message text and raw event payloads are stored unencrypted** in PostgreSQL.
  A database dump is as sensitive as the phone. Disk encryption is your job.
- **There is no read-only key.** A client key grants the full tool surface on
  its instance, including sending and deleting. Issue one per client so you can
  revoke one without revoking all.
- **There is one administrator**, not roles. Anyone with the panel password owns
  the WhatsApp session.
- **Retention is unbounded.** The database grows until you delete from it.
- **An agent with a key can be talked into sending messages** if you let
  untrusted text drive it unattended. The tools label content as data, but the
  final guard is the client's own approval flow.
- **Evolution Go is an unofficial WhatsApp integration** and is not covered by
  this policy. Report issues in it to
  [its repository](https://github.com/EvolutionAPI/evolution-go).

## Running it safely

- Put TLS in front of the panel. It publishes on loopback by default for a
  reason; `install.sh` sets up Traefik and Let's Encrypt if you want the short
  path.
- Keep exactly two ports public, 80 and 443, plus your own SSH port. Do it at
  your provider's firewall: `ufw` does not govern a published container port,
  because Docker's DNAT rules are evaluated before ufw's filter rules.
- Never publish Evolution, RabbitMQ, MinIO or PostgreSQL.
- Replace the installer's temporary password on first sign-in — the panel will
  make you.
- Keep `.env` at mode 600 and out of version control.
- Keep `MCP_STDIO` off on anything deployed: that transport carries no
  credential.
- Use a test WhatsApp account before a real one.
