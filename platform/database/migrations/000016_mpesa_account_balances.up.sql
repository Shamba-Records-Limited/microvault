-- Account Balance is async: the request names a shortcode, but the result
-- callback only carries an OriginatorConversationID. mpesa_balance_queries is
-- the correlation the ticker writes at request time and the callback reads
-- to know which shortcode a landing balance belongs to.

CREATE TABLE IF NOT EXISTS mpesa_balance_queries (
    originator_conversation_id varchar(100) PRIMARY KEY,
    shortcode integer NOT NULL,
    requested_at timestamp NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mpesa_account_balances (
    id uuid PRIMARY KEY,
    shortcode integer NOT NULL,
    account_name varchar(50) NOT NULL,
    currency varchar(10) NOT NULL,
    available_kes bigint NOT NULL,
    observed_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mpesa_account_balances_lookup
    ON mpesa_account_balances (shortcode, account_name, observed_at DESC);
