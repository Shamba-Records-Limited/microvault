DROP TABLE IF EXISTS counterparty_kyb_events;
DROP INDEX IF EXISTS idx_counterparties_kyb_session;
ALTER TABLE counterparties
    DROP COLUMN IF EXISTS kyb_session_at,
    DROP COLUMN IF EXISTS kyb_session_status,
    DROP COLUMN IF EXISTS kyb_session_url,
    DROP COLUMN IF EXISTS kyb_session_id,
    DROP COLUMN IF EXISTS kyb_provider;
