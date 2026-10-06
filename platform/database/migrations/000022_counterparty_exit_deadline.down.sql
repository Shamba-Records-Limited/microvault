DROP INDEX IF EXISTS idx_counterparty_addresses_exit_deadline;

ALTER TABLE counterparty_addresses
    DROP COLUMN IF EXISTS exit_warning_level,
    DROP COLUMN IF EXISTS exit_deadline,
    DROP COLUMN IF EXISTS frozen_at,
    DROP COLUMN IF EXISTS frozen_by;
