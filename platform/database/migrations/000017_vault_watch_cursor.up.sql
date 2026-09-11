-- Single-row cursor for the vault compliance watcher. Tracks the ledger
-- sequence the watcher has scanned up to, since getEvents is ledger-ranged
-- (see pkg/services/vaultwatch and pkg/stellar/rpc/events.go).

CREATE TABLE IF NOT EXISTS vault_watch_cursor (
    id smallint PRIMARY KEY DEFAULT 1,
    last_ledger integer NOT NULL,
    CONSTRAINT vault_watch_cursor_single_row CHECK (id = 1)
);

INSERT INTO vault_watch_cursor (id, last_ledger)
VALUES (1, 0)
ON CONFLICT (id) DO NOTHING;
