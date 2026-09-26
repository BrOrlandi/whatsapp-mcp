-- The newest release this database has ever been run by.
--
-- Migrations only move forward, so a database that a newer release has
-- touched may hold a schema an older one does not expect. Nothing stops an
-- older image from starting against it — a redeploy of a stale pin, an
-- update undone by hand — so the gateway remembers the highest version it
-- has seen, and one that starts below it says so instead of failing in some
-- corner later.
CREATE TABLE IF NOT EXISTS deployed_version (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    highest TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
