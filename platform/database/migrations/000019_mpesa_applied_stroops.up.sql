-- applied_stroops is set once, the first time a confirmed, loan-attributed
-- mpesa_transactions row is converted to USDC stroops and credited toward a
-- loan's repayment progress. Persisting the converted figure on the row
-- (rather than a running total on loans, recomputed from an FX rate that
-- drifts) makes the credit idempotent: a row is either unconverted (NULL,
-- eligible for the sweep) or converted at the rate that was live when it
-- actually happened, and re-running the sweep can never double-count or
-- re-price it.

ALTER TABLE mpesa_transactions ADD COLUMN IF NOT EXISTS applied_stroops bigint;

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_unapplied
    ON mpesa_transactions (loan_id)
    WHERE confirmed = true AND loan_id IS NOT NULL AND applied_stroops IS NULL;
