-- Revert the optional SEP-9 customer fields from users.
ALTER TABLE users
    DROP COLUMN IF EXISTS birth_date,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS postal_code,
    DROP COLUMN IF EXISTS state_or_province;
