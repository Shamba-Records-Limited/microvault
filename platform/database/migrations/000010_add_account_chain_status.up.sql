-- Track whether an account's child keypair actually exists on the Stellar
-- network.
--
-- Registration commits the user + account rows and then dispatches the
-- sponsored-creation transaction to a background goroutine, so a chain failure
-- leaves both rows present and status='active' with nothing on-chain. Until
-- now the only trace was a log line, which made the backlog of accounts needing
-- reconciliation invisible and unbounded.
--
-- States:
--   pending   — dispatched, not yet confirmed on-chain
--   confirmed — verified present on the network
--   failed    — creation retries exhausted; needs reconciliation
--   unknown   — predates this column; true state not established
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS chain_status VARCHAR(20) NOT NULL DEFAULT 'pending';

-- Existing rows were created before the column existed. Most are almost
-- certainly on-chain, but claiming 'confirmed' would assert something never
-- verified, and 'pending' would fabricate a backlog. 'unknown' is the honest
-- state; EnsureOnChainAccount resolves each one the first time it is touched.
UPDATE accounts SET chain_status = 'unknown';

-- Partial index for the ops sweep: the rows that need attention are a small
-- minority, so indexing only those keeps it cheap.
CREATE INDEX IF NOT EXISTS idx_accounts_chain_status_unresolved
    ON accounts (chain_status)
    WHERE chain_status IN ('pending', 'failed', 'unknown');
