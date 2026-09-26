-- Voice-note transcription through OpenAI's Whisper.
--
-- The key is the operator's own OpenAI credential, saved from the panel or
-- from an MCP client. It is an internal secret, same class as the Evolution
-- instance tokens and the licence key: it is never displayed back in full and
-- never returned to an MCP client.
CREATE TABLE IF NOT EXISTS transcription_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    openai_api_key TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every transcript is kept once it is paid for. A voice note never changes,
-- and asking Whisper again for the same audio would bill the operator twice
-- for an answer already in hand.
CREATE TABLE IF NOT EXISTS transcriptions (
    instance_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    text TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, message_id)
);
