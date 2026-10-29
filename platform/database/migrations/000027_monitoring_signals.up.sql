-- Monitoring alerts from screening and KYB providers. Each alert that names a
-- lender address queues one rescreen of that address; the outcome alerts a
-- person. Nothing is revoked automatically.
CREATE TABLE IF NOT EXISTS monitoring_signals (
    id                      uuid         PRIMARY KEY,
    provider                varchar(20)  NOT NULL,
    event                   varchar(80)  NOT NULL DEFAULT '',
    event_key               char(64)     NOT NULL,
    counterparty_id         uuid         REFERENCES counterparties(id),
    counterparty_address_id uuid         REFERENCES counterparty_addresses(id),
    status                  varchar(20)  NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'rescreened', 'failed', 'unmatched')),
    attempts                integer      NOT NULL DEFAULT 0,
    next_attempt_at         timestamptz  NOT NULL DEFAULT NOW(),
    last_error              text         NOT NULL DEFAULT '',
    verdict                 varchar(20)  NOT NULL DEFAULT '',
    screening_provider      varchar(40)  NOT NULL DEFAULT '',
    payload                 jsonb        NOT NULL,
    alerted_at              timestamptz,
    rescreened_at           timestamptz,
    received_at             timestamptz  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_monitoring_signals_event UNIQUE NULLS NOT DISTINCT (provider, event_key, counterparty_address_id)
);

CREATE INDEX IF NOT EXISTS idx_monitoring_signals_due ON monitoring_signals (next_attempt_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_monitoring_signals_address ON monitoring_signals (counterparty_address_id, received_at DESC);
