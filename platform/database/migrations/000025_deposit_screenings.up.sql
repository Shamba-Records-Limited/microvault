-- Vault deposit transaction screening. The vault watcher queues one
-- row per deposit event; the deposit screener screens it with the configured
-- providers. A bad verdict alerts a person; nothing is revoked automatically.
CREATE TABLE IF NOT EXISTS deposit_screenings (
    id                      uuid         PRIMARY KEY,
    event_id                varchar(64)  NOT NULL UNIQUE,
    tx_hash                 varchar(64)  NOT NULL,
    ledger                  bigint       NOT NULL,
    depositor               varchar(56)  NOT NULL,
    receiver                varchar(56)  NOT NULL DEFAULT '',
    counterparty_address_id uuid         REFERENCES counterparty_addresses(id),
    status                  varchar(20)  NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'screened', 'failed')),
    attempts                integer      NOT NULL DEFAULT 0,
    next_attempt_at         timestamptz  NOT NULL DEFAULT NOW(),
    last_error              text         NOT NULL DEFAULT '',
    provider                varchar(40)  NOT NULL DEFAULT '',
    provider_ref            varchar(120) NOT NULL DEFAULT '',
    verdict                 varchar(20)  NOT NULL DEFAULT '',
    sanctioned              boolean      NOT NULL DEFAULT false,
    risk_score              numeric,
    raw_payload             jsonb,
    alerted_at              timestamptz,
    screened_at             timestamptz,
    created_at              timestamptz  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deposit_screenings_due ON deposit_screenings (next_attempt_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_deposit_screenings_depositor ON deposit_screenings (depositor, created_at DESC);
