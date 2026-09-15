-- An installer that creates the administrator has to invent the first password,
-- and a password the machine chose and printed to a terminal is not a password
-- the operator has chosen. The flag makes the panel insist on a real one before
-- it will do anything else, and it is false for an account created by a person
-- in the setup form, who already chose.
ALTER TABLE control_panel_admin
    ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
