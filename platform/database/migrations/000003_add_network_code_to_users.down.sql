-- Remove network_code column from users table
DROP INDEX IF EXISTS idx_users_network_code;
ALTER TABLE users DROP COLUMN IF EXISTS network_code;
