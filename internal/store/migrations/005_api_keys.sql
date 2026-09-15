-- Credentials MCP clients present. The key itself is never stored: only its
-- SHA-256 digest, plus a prefix kept solely so the panel can show which key a
-- row refers to. A key is bound to exactly one instance, which is how the
-- client needs no instance name of its own.
CREATE TABLE IF NOT EXISTS api_keys (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS api_keys_hash_idx ON api_keys (key_hash) WHERE revoked_at IS NULL;
