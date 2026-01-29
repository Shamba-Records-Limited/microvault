-- Update user network-related fields for MoMo integration
-- Rename mobile_country_code to country_code and update default
ALTER TABLE users RENAME COLUMN mobile_country_code TO country_code;
ALTER TABLE users ALTER COLUMN country_code SET DEFAULT 'KE';

-- Rename network_code to mobile_network_code and increase size for longer codes
-- Also update default from '63902' (Safaricom) to '99999' (Africa's Talking sandbox)
DROP INDEX IF EXISTS idx_users_network_code;
ALTER TABLE users RENAME COLUMN network_code TO mobile_network_code;
ALTER TABLE users ALTER COLUMN mobile_network_code TYPE VARCHAR(6);
ALTER TABLE users ALTER COLUMN mobile_network_code SET DEFAULT '99999';
UPDATE users SET mobile_network_code = '99999' WHERE mobile_network_code = '63902';
CREATE INDEX idx_users_mobile_network_code ON users(mobile_network_code);

-- Add MoMo network fields for YellowCard integration
ALTER TABLE users ADD COLUMN momo_network_code VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN momo_network_name VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN telco_name VARCHAR(20) NOT NULL DEFAULT '';

-- Add indexes for MoMo network lookups
CREATE INDEX idx_users_momo_network_code ON users(momo_network_code);
