-- Webhooks: addresses that receive a POST for every new message, so a script
-- of the operator's can act on it as it arrives instead of polling the tools.
--
-- The secret signs every delivery (HMAC-SHA256 of the body) and is shown once,
-- when the webhook is created. It is an internal secret of the same class as
-- the Evolution instance tokens.
CREATE TABLE IF NOT EXISTS webhooks (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    events TEXT NOT NULL,
    include_own BOOLEAN NOT NULL DEFAULT FALSE,
    chats TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    disabled_reason TEXT NOT NULL DEFAULT '',
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_result TEXT NOT NULL DEFAULT '',
    delivered BIGINT NOT NULL DEFAULT 0
);
