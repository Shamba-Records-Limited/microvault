-- A counterparty's registered address. The country is the jurisdiction;
-- region is a first-level ISO 3166-2 code within it.
ALTER TABLE counterparties
    ADD COLUMN IF NOT EXISTS address_line1 varchar(200) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS address_line2 varchar(200) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS city          varchar(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS region        varchar(10)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS postal_code   varchar(20)  NOT NULL DEFAULT '';
