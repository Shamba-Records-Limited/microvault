DROP INDEX IF EXISTS idx_mpesa_transactions_unapplied;
ALTER TABLE mpesa_transactions DROP COLUMN IF EXISTS applied_stroops;
