-- Whether an activation link is sitting unanswered in the operator's inbox.
--
-- That is not the same as knowing their address: the address is collected when
-- the administrator account is created, while the link can only be requested
-- once Evolution is up and reporting that it has none. The wizard says "check
-- your email" only when this says a link is there to be checked, and clearing
-- it on a completed activation is what makes a licence that is later revoked
-- ask again instead of waiting forever on a click that already happened.
ALTER TABLE evolution_license ADD COLUMN IF NOT EXISTS link_sent_at TIMESTAMPTZ;
