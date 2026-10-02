DROP INDEX IF EXISTS idx_accounts_chain_heal_due;

ALTER TABLE accounts
    DROP COLUMN IF EXISTS chain_checked_at,
    DROP COLUMN IF EXISTS chain_attempts;
