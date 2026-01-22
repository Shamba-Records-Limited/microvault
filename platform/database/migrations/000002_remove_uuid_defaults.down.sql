-- Restore DEFAULT gen_random_uuid() to users table
ALTER TABLE users ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- Restore DEFAULT gen_random_uuid() to accounts table
ALTER TABLE accounts ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- Restore DEFAULT gen_random_uuid() to transactions table
ALTER TABLE transactions ALTER COLUMN id SET DEFAULT gen_random_uuid();
