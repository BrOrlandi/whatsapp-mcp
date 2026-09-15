-- Instances created through this control panel. The token is the credential
-- Evolution uses to resolve which instance a request targets, so it is an
-- internal secret: it never leaves the backend and never reaches an MCP client.
CREATE TABLE IF NOT EXISTS evolution_instances (
    instance_id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
