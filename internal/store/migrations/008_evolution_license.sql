-- The licence this deployment registered with Evolution's licensing server.
-- Evolution keeps its own copy in a Postgres volume a rebuild can lose, so the
-- panel keeps the credential it was issued to be able to reactivate without
-- asking the operator to register again. It is an internal secret, same class
-- as the Evolution instance tokens.
CREATE TABLE IF NOT EXISTS evolution_license (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    operator_email TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    customer_id INT NOT NULL DEFAULT 0,
    tier TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
