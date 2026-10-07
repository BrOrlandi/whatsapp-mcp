-- Small settings of the gateway that the panel and the tools change, such as
-- how many days downloaded media is kept.
CREATE TABLE IF NOT EXISTS gateway_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Conversations the operator marked as dealt with, or snoozed until a moment.
-- Kept here only: nothing is sent and the other side sees nothing. A message
-- from someone else after the mark brings the chat back to the lists.
CREATE TABLE IF NOT EXISTS triage_marks (
    instance_id TEXT NOT NULL,
    chat_jid TEXT NOT NULL,
    handled_at TIMESTAMPTZ NOT NULL,
    snoozed_until TIMESTAMPTZ,
    note TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (instance_id, chat_jid)
);

-- What the phone shows about each conversation: unread, archived, pinned,
-- muted. Evolution Go has no route that reads it, so it is assembled from the
-- events: the history sync names each conversation's unread count and flags,
-- the account's own read receipts (read-self) and its own messages clear the
-- unread, and the tools that organise a chat keep the flags.
--
-- read_until is the moment up to which the account has read the chat; every
-- message from someone else after it is unread. flags_at is when the flags
-- were last known: WhatsApp takes a chat out of the archive when a message
-- arrives, so an archive older than the chat's last message no longer holds.
CREATE TABLE IF NOT EXISTS chat_state (
    instance_id TEXT NOT NULL,
    chat_jid TEXT NOT NULL,
    read_until TIMESTAMPTZ,
    marked_unread BOOLEAN NOT NULL DEFAULT FALSE,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    muted_until TIMESTAMPTZ,
    name TEXT NOT NULL DEFAULT '',
    flags_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, chat_jid)
);
