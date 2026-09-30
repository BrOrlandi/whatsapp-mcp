# OAuth for ChatGPT and dots

OAuth is opt-in. Fixed API keys keep working when it is enabled. This server
registers one public client explicitly, using authorization code with S256
PKCE. It does not offer dynamic client registration or fetch client metadata
URLs. Select **predefined client credentials** in ChatGPT, with token endpoint
authentication **none** (public client, no client secret).

## Configure the existing installation

In the ChatGPT plugin's MCP settings, use the public `/mcp` URL and OAuth.
Set the client ID to `chatgpt`, leave the client secret empty, and copy the
exact production callback URL shown there. Add it to the existing `.env`:

```dotenv
PUBLIC_URL=https://wpp-mcp.on-forge.com
OAUTH_CLIENT_ID=chatgpt
OAUTH_REDIRECT_URIS=https://chatgpt.com/connector_platform_oauth_redirect
```

The callback above is ChatGPT's stable callback when issuer identification
is supported. This server advertises that support and includes `iss` on its
authorization responses. If the host shows a different callback, use that
exact URL instead. Multiple exact HTTPS callbacks may be comma-separated;
wildcards are not accepted. Do not put the old WhatsApp API key in a client
secret field. `PUBLIC_URL` must be an HTTPS origin without a path or query.

Build this modified checkout rather than pulling the upstream image. From the
existing Compose directory, using the same overlay files as the running stack:

```sh
docker compose build whatsapp-mcp
docker compose up -d --no-deps --pull never whatsapp-mcp
```

For an installation using the project's public Traefik overlay, both commands
must include `-f docker-compose.yml -f deploy/docker-compose.public.yml` before
`build` or `up`. Back up the gateway database before upgrading; the new migration
runs at startup. It adds OAuth tables and a nullable key expiry column.
Keep the existing `.env`, volumes and Compose project name.

Do not run `down -v`. Do not replace this local build with the upstream `edge`
image during subsequent updates: upstream does not contain this change yet.

## Connect the account

1. Open the panel, log in and select the intended WhatsApp instance.
2. In ChatGPT, connect the plugin account. Log in to the panel when prompted.
3. Confirm the instance name and permissions, then approve access.
4. Enable the connected plugin in the dot's tools.

The approval grants all existing tools, including sending, editing and deleting
messages. The selected instance is captured in the consent form; changing the
panel selection afterwards does not move an existing authorization.

Codes expire after five minutes and can be exchanged once. Access tokens expire
after at most one hour. Refresh tokens rotate on every use, replacing the prior
access token too. Reusing a consumed refresh token revokes the entire connection.
The authorization expires after 30 days even if tokens are refreshed. Disconnect
the `OAuth: chatgpt` connection in the panel to revoke it immediately. Tokens and
codes are stored only as hashes in PostgreSQL; restarting the gateway preserves
them, while existing panel sessions still require a fresh login.

## Verify and troubleshoot

```sh
curl -fsS https://wpp-mcp.on-forge.com/.well-known/oauth-protected-resource/mcp
curl -fsS https://wpp-mcp.on-forge.com/.well-known/oauth-authorization-server
curl -i https://wpp-mcp.on-forge.com/mcp
```

The first two requests must return JSON, and the third must return `401` with
a `WWW-Authenticate` header containing `resource_metadata`. The reverse proxy
must forward `/oauth/*`, `/.well-known/*` and `/mcp` to the gateway.

`invalid_request` on authorization usually means the client ID, callback,
resource or PKCE challenge differs from the registration. `invalid_target`
means `resource` is not exactly the public MCP URL. `invalid_grant` means a
code or token is expired, revoked, consumed or bound to another request. After
30 days or a revoked grant, reconnect through ChatGPT.

In a dot, test by asking for connection status or recent conversations without
sending a message. The automated suite verifies the protocol locally; a live
ChatGPT/dot connection must still be checked after deployment.

## Development check

Use a disposable PostgreSQL database; the test creates and removes its own
schema without touching existing schemas:

```sh
OAUTH_TEST_DATABASE_URL='postgres://user:password@localhost/test?sslmode=disable' go test -race ./internal/httpapi -run OAuth -v
```

Without that environment variable, the suite runs validation checks and skips
the PostgreSQL integration test. The integration check covers discovery, login,
consent, PKCE, instance/audience binding, restart persistence, concurrent code
redemption, expiry, refresh replay and revocation through OAuth and the panel.

Protocol references: [OpenAI OAuth requirements](https://developers.openai.com/plugins/build/auth),
[MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).
