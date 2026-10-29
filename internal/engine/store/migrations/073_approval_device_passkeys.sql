-- A passkey is checked against the page it was made on: its origin. A
-- phone keeps the attestation chain it enrolled with (JSON, base64 DER,
-- leaf first), as the record of what its key proved.
ALTER TABLE approval_devices ADD COLUMN origin TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_devices ADD COLUMN attestation TEXT NOT NULL DEFAULT '';
