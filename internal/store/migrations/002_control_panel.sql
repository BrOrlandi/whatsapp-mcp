CREATE TABLE IF NOT EXISTS control_panel_admin (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    username TEXT NOT NULL UNIQUE CHECK (length(username) BETWEEN 1 AND 100),
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS control_panel_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    selected_instance_id TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
