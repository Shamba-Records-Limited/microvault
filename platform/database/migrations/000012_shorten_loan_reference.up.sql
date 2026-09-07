-- Shorten loans.loan_reference and preserve the old form for dual-path
-- resolution.
--
-- The old format ("LR-unix_ms_hex-4_random_hex", 19 chars) overflows Daraja's
-- 12-character AccountReference cap on M-Pesa Express, and its embedded
-- timestamp made references enumerable — unacceptable once the reference
-- becomes the only binding between a paybill payment and a loan.
--
-- The new format is 2-char prefix + 6 random Crockford base32 + 1 check char
-- (e.g. "MV7K3QA9F"), derived deterministically from the immutable row id so a
-- re-run of this migration produces the same references instead of new ones.

ALTER TABLE loans
    ADD COLUMN IF NOT EXISTS legacy_loan_reference varchar(50);

-- Preserve every existing reference before rewriting.
UPDATE loans
    SET legacy_loan_reference = loan_reference
    WHERE legacy_loan_reference IS NULL
      AND loan_reference IS NOT NULL;

-- Deterministic Crockford base32 derivation from the row id.
--
-- digest(id, 'sha256') gives 32 bytes; the first six bytes each select one
-- alphabet character (byte & 31). The prefix is 'MV' — the configured
-- LOAN_REFERENCE_PREFIX at migration time. If the prefix is ever changed away
-- from 'MV', this migration must not be re-run unchanged; the check character
-- is bound to the prefix.
CREATE OR REPLACE FUNCTION loanref_crockford(seed uuid) RETURNS varchar
LANGUAGE sql IMMUTABLE AS $$
    SELECT 'MV'
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 0) & 31) + 1, 1)
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 1) & 31) + 1, 1)
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 2) & 31) + 1, 1)
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 3) & 31) + 1, 1)
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 4) & 31) + 1, 1)
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ', (get_byte(digest(seed::text, 'sha256'), 5) & 31) + 1, 1)
        -- check character: modulo-32 sum of the alphabet positions of the
        -- preceding 8 characters (prefix + 6), matching loanref.checkChar.
        || substr('0123456789ABCDEFGHJKMNPQRSTVWXYZ',
            (position('M' in '0123456789ABCDEFGHJKMNPQRSTVWXYZ') - 1
           + position('V' in '0123456789ABCDEFGHJKMNPQRSTVWXYZ') - 1
           + (get_byte(digest(seed::text, 'sha256'), 0) & 31)
           + (get_byte(digest(seed::text, 'sha256'), 1) & 31)
           + (get_byte(digest(seed::text, 'sha256'), 2) & 31)
           + (get_byte(digest(seed::text, 'sha256'), 3) & 31)
           + (get_byte(digest(seed::text, 'sha256'), 4) & 31)
           + (get_byte(digest(seed::text, 'sha256'), 5) & 31)) % 32 + 1,
            1)
$$;

UPDATE loans
    SET loan_reference = loanref_crockford(id)
    WHERE loan_reference IS NOT NULL
      AND loan_reference NOT LIKE 'MV_______';

-- Index the legacy column for the dual-path resolver.
CREATE INDEX IF NOT EXISTS idx_loans_legacy_loan_reference
    ON loans (legacy_loan_reference)
    WHERE legacy_loan_reference IS NOT NULL;

-- Uniqueness check: deterministic derivation from a uuid primary key cannot
-- collide in practice; abort loudly if it ever does.
DO $$
DECLARE
    dupes int;
BEGIN
    SELECT count(*) INTO dupes FROM (
        SELECT loan_reference FROM loans
        WHERE loan_reference IS NOT NULL
        GROUP BY loan_reference HAVING count(*) > 1
    ) d;
    IF dupes > 0 THEN
        RAISE EXCEPTION 'loan_reference collision after shortening: % duplicate groups', dupes;
    END IF;
END $$;

DROP FUNCTION loanref_crockford(uuid);
