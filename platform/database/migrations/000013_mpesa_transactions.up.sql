-- Staging log for inbound M-Pesa notifications, making the
-- confirm-before-credit discipline enforceable. Every callback lands here as an
-- observation; it becomes a payment only after ConfirmedVia names an
-- independent check. Daraja signs nothing, so a callback is never evidence.

CREATE TABLE IF NOT EXISTS mpesa_transactions (
    id uuid PRIMARY KEY,
    trans_id varchar(20) NOT NULL,
    source varchar(20) NOT NULL,
    confirmed boolean NOT NULL DEFAULT false,
    confirmed_via varchar(20),
    bill_ref_number varchar(20),
    loan_id uuid,
    amount_kes bigint NOT NULL,
    msisdn_masked varchar(25),
    msisdn_full varchar(25),
    payer_name varchar(100),
    trans_time timestamp NOT NULL,
    third_party_trans_id varchar(100),
    checkout_request_id varchar(100),
    merchant_request_id varchar(100),
    sequence_id varchar(200),
    raw_payload jsonb NOT NULL,
    reversal_state varchar(20) NOT NULL DEFAULT 'none',
    next_poll_at timestamp,
    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

-- The idempotency key: one M-Pesa receipt settles one observation, and a
-- replay is a database no-op rather than a double credit.
CREATE UNIQUE INDEX IF NOT EXISTS idx_mpesa_transactions_trans_id
    ON mpesa_transactions (trans_id);

-- The STK poller's due set: unconfirmed rows with a poll moment that has
-- come due. Partial because scanning the whole table per tick defeats the
-- point of next_poll_at.
CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_poll
    ON mpesa_transactions (next_poll_at)
    WHERE next_poll_at IS NOT NULL AND confirmed = false;

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_loan_id
    ON mpesa_transactions (loan_id);

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_bill_ref
    ON mpesa_transactions (bill_ref_number);

CREATE INDEX IF NOT EXISTS idx_mpesa_transactions_checkout
    ON mpesa_transactions (checkout_request_id);
