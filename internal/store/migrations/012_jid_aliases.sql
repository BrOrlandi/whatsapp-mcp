-- WhatsApp is moving one-to-one chats from phone-number JIDs
-- (5511…@s.whatsapp.net) to LIDs (1528…@lid), identifiers that hide the
-- number. The same conversation then arrives under both: the history under
-- the number, the newest messages under the LID, and a reader of either sees
-- half of it.
--
-- Every LID message still says which number it belongs to — a live event
-- carries it in SenderAlt or RecipientAlt, a history sync in the
-- conversation's pnJID — so the pairing is recorded here and the reading
-- queries treat both JIDs as one chat. The messages themselves keep the JID
-- WhatsApp used: deleting, editing, reacting and paging history all have to
-- address a message the way WhatsApp knows it.
CREATE TABLE IF NOT EXISTS jid_aliases (
    instance_id TEXT NOT NULL,
    lid TEXT NOT NULL,
    pn TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, lid)
);

CREATE INDEX IF NOT EXISTS jid_aliases_pn_idx ON jid_aliases (instance_id, pn);

-- Backfill from what is already indexed. Only LID messages are read, and for
-- each LID the most recently received pairing wins.
INSERT INTO jid_aliases (instance_id, lid, pn)
SELECT DISTINCT ON (instance_id, lid) instance_id, lid, pn
  FROM (
        SELECT m.instance_id, m.chat_jid AS lid,
               CASE WHEN (e.payload->'data'->'Info'->>'IsFromMe')::boolean
                    THEN e.payload->'data'->'Info'->>'RecipientAlt'
                    ELSE e.payload->'data'->'Info'->>'SenderAlt' END AS pn,
               e.received_at
          FROM messages m JOIN events e ON e.event_id = m.event_id
         WHERE m.chat_jid LIKE '%@lid' AND e.event_type IN ('Message', 'SendMessage')
        UNION ALL
        SELECT DISTINCT m.instance_id, m.chat_jid, c->>'pnJID', e.received_at
          FROM messages m JOIN events e ON e.event_id = m.event_id,
               jsonb_array_elements(e.payload->'data'->'Data'->'conversations') c
         WHERE m.chat_jid LIKE '%@lid' AND e.event_type = 'HistorySync'
           AND c->>'ID' = m.chat_jid
       ) pairs
 WHERE pn LIKE '%@s.whatsapp.net' AND instance_id <> ''
 ORDER BY instance_id, lid, received_at DESC
ON CONFLICT (instance_id, lid) DO NOTHING;
