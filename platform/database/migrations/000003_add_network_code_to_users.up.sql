-- Add network_code column to users table
-- Stores Africa's Talking network code (e.g., '99999' for Africa's Talking Athena sandbox network)
ALTER TABLE users ADD COLUMN network_code VARCHAR(5) NOT NULL DEFAULT '99999';

-- Add index for network_code lookups
CREATE INDEX idx_users_network_code ON users(network_code);
