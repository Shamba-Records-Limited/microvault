-- M-Pesa repayment state on loans, mirroring the MoneyGram shape established
-- by repayment_mg_tx_id / repayment_next_poll_at. repayment_provider is the
-- discriminator that keeps the two pollers from driving each other's loans.

ALTER TABLE loans
    ADD COLUMN IF NOT EXISTS repayment_provider varchar(20),
    ADD COLUMN IF NOT EXISTS repayment_mpesa_checkout_id varchar(100),
    ADD COLUMN IF NOT EXISTS repayment_mpesa_trans_id varchar(20),
    ADD COLUMN IF NOT EXISTS repayment_stk_attempts int NOT NULL DEFAULT 0;

-- In-flight MoneyGram repayments belong to the moneygram driver.
UPDATE loans
    SET repayment_provider = 'moneygram'
    WHERE repayment_provider IS NULL
      AND repayment_mg_tx_id IS NOT NULL
      AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_loans_repayment_mpesa_checkout_id
    ON loans (repayment_mpesa_checkout_id)
    WHERE repayment_mpesa_checkout_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_loans_repayment_mpesa_trans_id
    ON loans (repayment_mpesa_trans_id)
    WHERE repayment_mpesa_trans_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_loans_repayment_stk_due
    ON loans (repayment_next_poll_at)
    WHERE deleted_at IS NULL
      AND repayment_status = 'initiated'
      AND repayment_provider = 'mpesa'
      AND repayment_mpesa_checkout_id IS NOT NULL;
