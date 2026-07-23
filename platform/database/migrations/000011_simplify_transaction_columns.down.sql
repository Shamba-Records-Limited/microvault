-- Restores the schema shape only. The dropped values are not recoverable:
-- stellar_status came from an RPC response and tx_category from the caller, and
-- neither is reconstructible from what remains. Rows restored by this migration
-- carry the column defaults, so tx_category will read 'on_chain' even for rows
-- whose tx_type settles off-chain.
DROP INDEX IF EXISTS idx_transactions_one_payout_per_loan;

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS stellar_status VARCHAR(20),
    ADD COLUMN IF NOT EXISTS tx_category VARCHAR(20) NOT NULL DEFAULT 'on_chain';

CREATE INDEX IF NOT EXISTS idx_transactions_tx_category ON transactions(tx_category);
