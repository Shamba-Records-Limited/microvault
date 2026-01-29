-- Revert default values for MoMo network fields
ALTER TABLE users ALTER COLUMN momo_network_code SET DEFAULT '';
ALTER TABLE users ALTER COLUMN momo_network_name SET DEFAULT '';
ALTER TABLE users ALTER COLUMN telco_name SET DEFAULT '';

-- Note: We don't revert the data values as we don't know what the original values were
