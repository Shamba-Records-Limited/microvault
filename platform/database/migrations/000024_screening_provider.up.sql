-- Screening provider toggle (vault doc Security & Compliance/screening-provider-toggle-plan.md):
-- which provider produced each screening ("elliptic", "merkle", "didit", or a
-- "+"-joined list for a combined screening) and the provider's own handle on it.
ALTER TABLE address_screenings
    ADD COLUMN IF NOT EXISTS provider     varchar(40)  NOT NULL DEFAULT 'elliptic',
    ADD COLUMN IF NOT EXISTS provider_ref varchar(120) NOT NULL DEFAULT '';
