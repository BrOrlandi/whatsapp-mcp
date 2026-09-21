-- Which AI tool a credential ended up in.
--
-- The panel talks about connections, not keys: an operator who pasted the
-- configuration into Claude Desktop wants to read "Claude Desktop", not a
-- label they typed and a hash prefix. The MCP handshake already carries the
-- answer — every client announces itself in the `initialize` request — so it
-- is recorded the first time a credential is used and refreshed on every
-- later handshake, which is also how a key moved to another tool corrects
-- itself instead of lying.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS client_name TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS client_version TEXT NOT NULL DEFAULT '';
