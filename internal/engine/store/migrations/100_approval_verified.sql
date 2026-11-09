-- An approval that needs a signed decision whose device verified the person
-- (fingerprint, PIN): an allow whose verification is only presence (a
-- touch) is refused. verification is added to the audit chain's rows for
-- decisions.
ALTER TABLE approval_requests ADD COLUMN verified INTEGER NOT NULL DEFAULT 0;
ALTER TABLE credential_audit ADD COLUMN verification TEXT NOT NULL DEFAULT '';
