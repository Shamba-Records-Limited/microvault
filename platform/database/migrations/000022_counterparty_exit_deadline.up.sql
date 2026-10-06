-- Exit grace period and freeze for revoked vault depositors. frozen_at is a
-- freeze intent the on-chain writer turns into freeze_depositor, as
-- revoked_at is for disallow_depositor. exit_deadline mirrors the vault's
-- ExitDeadline for the address; exit_warning_level records which "window
-- closing" alerts have been sent for it (0 none, 1 seven-day, 2 one-day) and
-- resets whenever the deadline moves.

ALTER TABLE counterparty_addresses
    ADD COLUMN IF NOT EXISTS frozen_by text,
    ADD COLUMN IF NOT EXISTS frozen_at timestamp,
    ADD COLUMN IF NOT EXISTS exit_deadline timestamp,
    ADD COLUMN IF NOT EXISTS exit_warning_level smallint NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_counterparty_addresses_exit_deadline
    ON counterparty_addresses (exit_deadline)
    WHERE exit_deadline IS NOT NULL;
