-- Drop two columns that could not carry information, and replace a
-- uniqueness rule that stated the wrong invariant.
--
-- stellar_status held the Soroban RPC result string, but every write site set
-- it in the same UPDATE that set status='success'. The two could never
-- disagree, so it recorded nothing status did not already say. Where the
-- ledger's own verdict is wanted it can be re-read from stellar_tx_hash.
--
-- tx_category was a total function of tx_type — on_chain for vault_borrow,
-- vault_repay, anchor_transfer and refund; off_chain for off_ramp and
-- fiat_failover — and was never queried. Validation only checked the value was
-- a member of the enum, so a row could claim a category contradicting its own
-- type and pass. It is now derived in code (models.TxCategoryFor).
DROP INDEX IF EXISTS idx_transactions_tx_category;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS stellar_status,
    DROP COLUMN IF EXISTS tx_category;

-- external_id becomes non-unique by design.
--
-- It now answers "which provider transaction does this row belong to", holding
-- the provider's request ID. One MoneyGram cash pickup settles as three legs —
-- anchor_transfer, off_ramp, refund — which all share that ID, so uniqueness
-- was incompatible with recording the full lifecycle. The application-level
-- uniqueness check is removed with this migration; the index below replaces the
-- guarantee it was actually providing.
--
-- The real invariant is one fiat payout per loan. On-chain rows are excluded:
-- a send that fails on-ledger and is retried produces two legitimate rows with
-- distinct hashes, and stellar_tx_hash's unique constraint already blocks true
-- duplicates there.
CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_one_payout_per_loan
    ON transactions (loan_id, tx_type)
    WHERE tx_type IN ('off_ramp', 'fiat_failover');
