-- KYB compliance persistence: an institutional depositor (counterparty),
-- the Stellar addresses it submits, and the append-only history of every
-- Elliptic screening performed on those addresses. See
-- elliptic-compliance-integration.md §12 in the knowledge vault.
--
-- All three tables are core-owned end to end, unlike mgpoller's tables
-- (which deliberately skip FKs because they reference credit-owned rows) —
-- FKs between these three are safe and used.

CREATE TABLE IF NOT EXISTS counterparties (
    id uuid PRIMARY KEY,
    legal_name varchar(200) NOT NULL,
    registration_number varchar(100),
    jurisdiction varchar(100),
    kyb_status varchar(20) NOT NULL DEFAULT 'pending',
    elliptic_customer_reference varchar(100) NOT NULL,
    kyb_approved_at timestamp,
    -- Stellar public key as text, not a user-UUID FK — the admin's audit
    -- columns for this new work follow the decision the templ-admin plan
    -- already made but never migrated the legacy tables onto.
    kyb_approved_by text,
    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_counterparties_elliptic_customer_reference
    ON counterparties (elliptic_customer_reference);

CREATE INDEX IF NOT EXISTS idx_counterparties_kyb_status
    ON counterparties (kyb_status);

CREATE TABLE IF NOT EXISTS counterparty_addresses (
    id uuid PRIMARY KEY,
    counterparty_id uuid NOT NULL REFERENCES counterparties(id),
    -- Stellar G... addresses are 56 characters, matching accounts.public_key.
    address varchar(56) NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'pending',
    last_screening_id uuid,
    screened_at timestamp,
    expires_at timestamp,
    onchain_state varchar(20) NOT NULL DEFAULT 'absent',
    approved_by text,
    approved_at timestamp,
    revoked_by text,
    revoked_at timestamp,
    override_reason text,
    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_counterparty_addresses_address
    ON counterparty_addresses (address);

CREATE INDEX IF NOT EXISTS idx_counterparty_addresses_counterparty_id
    ON counterparty_addresses (counterparty_id);

-- The screening queue's due set: review/pending/expired rows, per §14's
-- "screening queue" admin screen.
CREATE INDEX IF NOT EXISTS idx_counterparty_addresses_status
    ON counterparty_addresses (status);

CREATE INDEX IF NOT EXISTS idx_counterparty_addresses_onchain_state
    ON counterparty_addresses (onchain_state);

CREATE INDEX IF NOT EXISTS idx_counterparty_addresses_expires_at
    ON counterparty_addresses (expires_at)
    WHERE expires_at IS NOT NULL;

-- Append-only: every screening ever performed. No update path, ever — a
-- compliance record that changes in place cannot answer "what did we know
-- on the day we approved this."
CREATE TABLE IF NOT EXISTS address_screenings (
    id uuid PRIMARY KEY,
    address_id uuid NOT NULL REFERENCES counterparty_addresses(id),
    elliptic_analysis_id varchar(100),
    elliptic_screening_id varchar(100),
    screening_source varchar(30) NOT NULL,
    -- Nullable, never defaulted to zero — a null score is not a clean score.
    risk_score numeric,
    sanctioned boolean NOT NULL DEFAULT false,
    verdict varchar(20) NOT NULL,
    raw_payload jsonb NOT NULL,
    created_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_address_screenings_address_id
    ON address_screenings (address_id);
