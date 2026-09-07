DROP INDEX IF EXISTS idx_loans_repayment_stk_due;
DROP INDEX IF EXISTS uq_loans_repayment_mpesa_trans_id;
DROP INDEX IF EXISTS uq_loans_repayment_mpesa_checkout_id;

ALTER TABLE loans
    DROP COLUMN IF EXISTS repayment_stk_attempts,
    DROP COLUMN IF EXISTS repayment_mpesa_trans_id,
    DROP COLUMN IF EXISTS repayment_mpesa_checkout_id,
    DROP COLUMN IF EXISTS repayment_provider;
