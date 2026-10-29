-- Lender KYB verification through Didit. The counterparty row holds the
-- current session and its latest status; every status change Didit reports
-- is also kept, append-only, in counterparty_kyb_events.
ALTER TABLE counterparties
    ADD COLUMN IF NOT EXISTS kyb_provider       varchar(20)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS kyb_session_id     varchar(80),
    ADD COLUMN IF NOT EXISTS kyb_session_url    text         NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS kyb_session_status varchar(20)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS kyb_session_at     timestamptz;

CREATE UNIQUE INDEX IF NOT EXISTS idx_counterparties_kyb_session ON counterparties (kyb_session_id) WHERE kyb_session_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS counterparty_kyb_events (
    id               uuid         PRIMARY KEY,
    counterparty_id  uuid         NOT NULL REFERENCES counterparties(id),
    provider         varchar(20)  NOT NULL,
    session_id       varchar(80)  NOT NULL,
    event            varchar(40)  NOT NULL,
    status           varchar(20)  NOT NULL,
    previous_status  varchar(20)  NOT NULL DEFAULT '',
    payload          jsonb        NOT NULL,
    received_at      timestamptz  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_counterparty_kyb_events_counterparty ON counterparty_kyb_events (counterparty_id, received_at DESC);
