-- Fields the Evolution Go payload actually carries. The first slice parsed the
-- Evolution API v2 shape and left these empty, which also left instance_id
-- empty and made every search return nothing.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sender_jid TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_group BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_type TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS messages_instance_sent_idx ON messages (instance_id, sent_at DESC NULLS LAST);
