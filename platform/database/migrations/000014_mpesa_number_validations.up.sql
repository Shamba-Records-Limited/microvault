-- Caches Mobile Number Validation verdicts, keyed on a hash of the
-- (msisdn, idType, idNumber) tuple rather than the tuple itself, so the cache
-- is not itself a store of national ID numbers.

CREATE TABLE IF NOT EXISTS mpesa_number_validations (
    id uuid PRIMARY KEY,
    identity_hash varchar(64) NOT NULL,
    matched boolean NOT NULL,
    response_code varchar(10) NOT NULL,
    checked_at timestamp NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mpesa_number_validations_identity_hash
    ON mpesa_number_validations (identity_hash);
