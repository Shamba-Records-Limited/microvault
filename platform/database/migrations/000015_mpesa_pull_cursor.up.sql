-- Single-row cursor for the Pull reconciliation sweep. The Pull sweep walks
-- a wall-clock window (see pkg/services/mpesapoller), not a queue of rows, so
-- its cadence state is "how far have we swept" rather than a per-row
-- next_poll_at.

CREATE TABLE IF NOT EXISTS mpesa_pull_cursor (
    id smallint PRIMARY KEY DEFAULT 1,
    swept_to timestamp NOT NULL,
    CONSTRAINT mpesa_pull_cursor_single_row CHECK (id = 1)
);

INSERT INTO mpesa_pull_cursor (id, swept_to)
VALUES (1, now())
ON CONFLICT (id) DO NOTHING;
