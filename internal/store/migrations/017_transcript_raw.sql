-- A transcript corrected against its conversation keeps what speech
-- recognition first heard, so the correction can be checked.
ALTER TABLE transcriptions ADD COLUMN IF NOT EXISTS raw_text TEXT NOT NULL DEFAULT '';
