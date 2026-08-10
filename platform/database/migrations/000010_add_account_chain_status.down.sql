DROP INDEX IF EXISTS idx_accounts_chain_status_unresolved;

ALTER TABLE accounts
    DROP COLUMN IF EXISTS chain_status;
