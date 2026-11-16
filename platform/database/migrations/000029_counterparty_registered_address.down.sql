ALTER TABLE counterparties
    DROP COLUMN IF EXISTS address_line1,
    DROP COLUMN IF EXISTS address_line2,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS postal_code;
