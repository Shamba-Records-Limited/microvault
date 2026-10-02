-- Retry state for the account chain reconciler, which heals accounts whose
-- sponsored on-chain creation failed, stalled in pending, or predates
-- chain_status. chain_attempts counts failed heals; chain_checked_at is the
-- last heal attempt and drives the retry spacing. 'conflict' rows (a reused
-- derivation index) are never selected.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS chain_attempts int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chain_checked_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_accounts_chain_heal_due
    ON accounts (chain_checked_at)
    WHERE deleted_at IS NULL
      AND chain_status IN ('pending', 'failed', 'unknown');
