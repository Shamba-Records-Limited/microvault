ALTER TABLE address_screenings
    DROP COLUMN IF EXISTS provider_ref,
    DROP COLUMN IF EXISTS provider;
