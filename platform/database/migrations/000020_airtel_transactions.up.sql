-- Staging log for inbound Airtel Money notifications, the sibling of
-- mpesa_transactions and deliberately not a generalisation of it: the two
-- rails disagree about what a receipt is, when it exists, and what a callback
-- proves, and one table would have to lie about all three.
--
-- Three differences from mpesa_transactions drive the shape here:
--
-- The key is our own id, not the receipt. airtel_money_id is minted only on
-- TS and is absent on TIP and TF, so it cannot be the NOT NULL unique column
-- that trans_id is next door. partner_txn_id — the id we generated and sent —
-- is present from the first callback and is what an enquiry is keyed by.
--
-- A transaction can report more than once. Airtel documents its callback as
-- carrying intermediate or final status, so a row is updated in place across
-- callbacks rather than inserted per notification. The unique index makes the
-- second callback an update, not a conflict to be swallowed.
--
-- A callback can be authenticated. Unlike Daraja, Airtel signs with an
-- HmacSHA256 when callback authentication is enabled. hash_verified records
-- whether it checked out and hash_variant records which reading of "the body"
-- matched, because the portal does not say which one Airtel uses and the
-- first live callback is what settles it.
--
-- None of that makes a row evidence. It becomes a payment only once
-- confirmed_via names an independent enquiry.

CREATE TABLE IF NOT EXISTS airtel_transactions (
    id uuid PRIMARY KEY,
    partner_txn_id varchar(64) NOT NULL,
    airtel_money_id varchar(64),
    source varchar(20) NOT NULL,
    status_code varchar(4) NOT NULL,
    confirmed boolean NOT NULL DEFAULT false,
    confirmed_via varchar(20),
    hash_verified boolean NOT NULL DEFAULT false,
    hash_variant varchar(20),
    reference varchar(25),
    loan_id uuid,
    amount_minor bigint NOT NULL,
    applied_stroops bigint,
    msisdn varchar(25),
    payer_name varchar(100),
    trans_time timestamp NOT NULL,
    raw_payload jsonb NOT NULL,
    next_poll_at timestamp,
    poll_attempts integer NOT NULL DEFAULT 0,
    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

-- The idempotency key. One transaction has one row however many callbacks
-- Airtel sends for it, which is what makes an intermediate-then-final pair an
-- update rather than a duplicate credit.
CREATE UNIQUE INDEX IF NOT EXISTS idx_airtel_transactions_partner_txn_id
    ON airtel_transactions (partner_txn_id);

-- The receipt is nullable and only unique among rows that have one. A partial
-- unique index says exactly that; a plain unique index would let two settled
-- transactions share a receipt as long as one of them was still NULL.
CREATE UNIQUE INDEX IF NOT EXISTS idx_airtel_transactions_money_id
    ON airtel_transactions (airtel_money_id)
    WHERE airtel_money_id IS NOT NULL;

-- The enquiry poller's due set. Partial, because scanning the whole table per
-- tick defeats the point of next_poll_at.
CREATE INDEX IF NOT EXISTS idx_airtel_transactions_poll
    ON airtel_transactions (next_poll_at)
    WHERE next_poll_at IS NOT NULL AND confirmed = false;

CREATE INDEX IF NOT EXISTS idx_airtel_transactions_loan_id
    ON airtel_transactions (loan_id);

CREATE INDEX IF NOT EXISTS idx_airtel_transactions_reference
    ON airtel_transactions (reference);

-- The sweep queue: confirmed, loan-attributed rows not yet converted to
-- stroops. Mirrors idx_mpesa_transactions_unapplied.
CREATE INDEX IF NOT EXISTS idx_airtel_transactions_unapplied
    ON airtel_transactions (loan_id)
    WHERE confirmed = true AND loan_id IS NOT NULL AND applied_stroops IS NULL;

-- Single-row cursor for the Transactions Summary reconciliation sweep, which
-- walks a wall-clock window rather than a queue of rows. The Airtel analogue
-- of mpesa_pull_cursor.
CREATE TABLE IF NOT EXISTS airtel_summary_cursor (
    id smallint PRIMARY KEY DEFAULT 1,
    swept_to timestamp NOT NULL,
    CONSTRAINT airtel_summary_cursor_single_row CHECK (id = 1)
);

INSERT INTO airtel_summary_cursor (id, swept_to)
VALUES (1, now())
ON CONFLICT (id) DO NOTHING;
