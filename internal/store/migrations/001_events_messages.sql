CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    instance_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL,
    chat_jid TEXT NOT NULL DEFAULT '',
    sender_name TEXT NOT NULL DEFAULT '',
    from_me BOOLEAN NOT NULL DEFAULT FALSE,
    text TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    event_id TEXT NOT NULL REFERENCES events(event_id),
    search_vector TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', coalesce(text, ''))) STORED,
    PRIMARY KEY (instance_id, message_id)
);

CREATE INDEX IF NOT EXISTS messages_search_idx ON messages USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS messages_chat_sent_idx ON messages (chat_jid, sent_at DESC);
