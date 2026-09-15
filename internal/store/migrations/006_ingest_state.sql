-- Tracks which decoder version produced the rows in `messages`.
--
-- `messages` is a projection of `events`: the raw payload is the record of what
-- happened, and every column in `messages` is derived from it. When the decoder
-- is corrected, the projection is stale rather than the data lost, and the
-- gateway rebuilds it from the payloads it already holds.
CREATE TABLE IF NOT EXISTS ingest_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    decoder_version INTEGER NOT NULL DEFAULT 0,
    reprojected_at TIMESTAMPTZ,
    reprojected_events BIGINT NOT NULL DEFAULT 0,
    reprojected_messages BIGINT NOT NULL DEFAULT 0
);

INSERT INTO ingest_state (singleton) VALUES (TRUE) ON CONFLICT (singleton) DO NOTHING;

-- Paging through the events in a stable order is what lets a reprojection
-- resume instead of starting over.
CREATE INDEX IF NOT EXISTS events_received_idx ON events (received_at, event_id);
