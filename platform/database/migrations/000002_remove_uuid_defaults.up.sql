-- Remove DEFAULT gen_random_uuid() from users table
-- IDs are now generated at application layer using UUIDv7
ALTER TABLE users ALTER COLUMN id DROP DEFAULT;

-- Remove DEFAULT gen_random_uuid() from accounts table
ALTER TABLE accounts ALTER COLUMN id DROP DEFAULT;

-- Remove DEFAULT gen_random_uuid() from transactions table
ALTER TABLE transactions ALTER COLUMN id DROP DEFAULT;
