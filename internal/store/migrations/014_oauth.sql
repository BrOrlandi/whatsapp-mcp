-- OAuth access credentials reuse the panel's existing key revocation controls.
-- NULL means a manually issued API key, whose lifetime is unchanged.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS oauth_grants (
    key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    resource TEXT NOT NULL,
    challenge TEXT NOT NULL,
    code_hash TEXT UNIQUE,
    code_expires_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Retaining consumed hashes detects replay and revokes the entire grant.
CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    key_id BIGINT NOT NULL REFERENCES oauth_grants(key_id) ON DELETE CASCADE,
    used_at TIMESTAMPTZ
);
