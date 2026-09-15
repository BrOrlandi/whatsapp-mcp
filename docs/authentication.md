# Authentication

Two credentials exist, for two different audiences, and they are deliberately
not the same kind of secret.

## Client keys

A client needs exactly one thing: an API key. The key identifies the account and
the WhatsApp instance it is authorised for, so there is no user, no password and
no instance name to configure.

Keys are `wamcp-` followed by 24 alphanumeric characters, around 142 bits of
entropy. Only the SHA-256 digest is stored — SHA-256 rather than bcrypt because
bcrypt's deliberate cost protects a human-chosen password against a dictionary,
and would add roughly 100 ms to every MCP request here while guarding against an
attack that cannot succeed against 142 random bits.

The endpoint has no anonymous mode:

- A credential is accepted only in the `Authorization` header, never in a query
  string, where it would end up in proxy logs and browser history.
- A rejected credential gets a `401` that does not distinguish an unknown key
  from a revoked one.
- Repeated failures are throttled per source address.
- Revocation takes effect immediately, because each request is authenticated on
  its own.

That statelessness is also what makes the endpoint resilient: there is no
session to resume, so a dropped connection costs nothing.

Issue one key per client. That is what lets you revoke the laptop without
knocking the desktop offline, and what makes "last used" mean something.

## The panel account

The administrator password is bcrypt-hashed in PostgreSQL — bcrypt here, because
this *is* a human-chosen password facing a dictionary.

Sessions use signed, HttpOnly, SameSite cookies with concurrency-safe
server-side state. A process restart intentionally invalidates active sessions.

The first visit to a fresh install creates the account. There is no default
password.

## The instance token

The panel mints a per-instance Evolution token and stores it in PostgreSQL,
because Evolution resolves the target instance from the key on the request and
accepts no instance parameter. It is an internal secret: never displayed, never
returned by a tool, never sent to an MCP client.

The global `EVOLUTION_API_KEY` is used only for the administrative routes
(`/instance/create`, `/instance/all`, `/instance/delete/{id}`).

## What a key holder can do

An API key grants the full tool surface on the instance it is bound to: reading
every indexed conversation, sending, deleting and editing the account's own
messages, and reacting to anyone's. There is no read-only key today.

Treat a client key as equivalent to handing someone the phone.
