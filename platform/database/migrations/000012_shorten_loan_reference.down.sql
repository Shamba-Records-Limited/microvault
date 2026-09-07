-- Restore the original references and drop the legacy column.
UPDATE loans
    SET loan_reference = legacy_loan_reference
    WHERE legacy_loan_reference IS NOT NULL;

DROP INDEX IF EXISTS idx_loans_legacy_loan_reference;

ALTER TABLE loans
    DROP COLUMN IF EXISTS legacy_loan_reference;
