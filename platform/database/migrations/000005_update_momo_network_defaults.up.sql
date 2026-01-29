-- Update default values for MoMo network fields
ALTER TABLE users ALTER COLUMN momo_network_code SET DEFAULT 'SANDBOX';
ALTER TABLE users ALTER COLUMN momo_network_name SET DEFAULT 'Sandbox Network';
ALTER TABLE users ALTER COLUMN telco_name SET DEFAULT 'Athena';

-- Update existing empty values to new defaults
UPDATE users SET momo_network_code = 'SANDBOX' WHERE momo_network_code = '';
UPDATE users SET momo_network_name = 'Sandbox Network' WHERE momo_network_name = '';
UPDATE users SET telco_name = 'Athena' WHERE telco_name = '';
