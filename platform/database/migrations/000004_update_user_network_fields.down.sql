-- Revert user network-related field changes
-- Remove MoMo network fields
DROP INDEX IF EXISTS idx_users_momo_network_code;
ALTER TABLE users DROP COLUMN IF EXISTS telco_name;
ALTER TABLE users DROP COLUMN IF EXISTS momo_network_name;
ALTER TABLE users DROP COLUMN IF EXISTS momo_network_code;

-- Revert mobile_network_code back to network_code
-- Revert default from '99999' back to '63902'
DROP INDEX IF EXISTS idx_users_mobile_network_code;
ALTER TABLE users RENAME COLUMN mobile_network_code TO network_code;
ALTER TABLE users ALTER COLUMN network_code TYPE VARCHAR(5);
ALTER TABLE users ALTER COLUMN network_code SET DEFAULT '63902';
UPDATE users SET network_code = '63902' WHERE network_code = '99999';
CREATE INDEX idx_users_network_code ON users(network_code);

-- Revert country_code back to mobile_country_code
ALTER TABLE users RENAME COLUMN country_code TO mobile_country_code;
ALTER TABLE users ALTER COLUMN mobile_country_code SET DEFAULT '254';
