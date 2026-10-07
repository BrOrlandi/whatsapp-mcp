-- What a message says about other messages and people: the message it
-- quotes, who it mentions, whether it was forwarded, the message a reaction
-- is to, the file a media message carries, and whether it was later edited or
-- deleted. Rows ingested before this are filled in from their stored events
-- by the enrichment pass at startup.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS quoted_id TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS mentions TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS forwarded BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reaction_to TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reaction TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS mime_type TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS filename TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS edited BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked BOOLEAN NOT NULL DEFAULT FALSE;
-- Which decoder version filled a row's details, so the enrichment pass knows
-- what is left to do and can resume.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS details_version SMALLINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS messages_sender_idx ON messages (instance_id, sender_jid, sent_at DESC);
CREATE INDEX IF NOT EXISTS messages_chat_from_me_idx ON messages (instance_id, chat_jid, from_me, sent_at DESC);
CREATE INDEX IF NOT EXISTS messages_details_pending_idx ON messages (event_id) WHERE details_version < 3;

-- Unread counts start from what the index can know. Before this release no
-- read receipt was recorded, so a chat without any read information counts
-- only the messages received from this moment on.
INSERT INTO gateway_settings (key, value) VALUES ('unread_tracking_since', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
ON CONFLICT (key) DO NOTHING;
