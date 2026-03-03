-- Remove security questions table
DROP TABLE IF EXISTS security_questions;

-- Remove PIN management fields from users table
ALTER TABLE users
    DROP COLUMN IF EXISTS pin_hash,
    DROP COLUMN IF EXISTS pin_attempts,
    DROP COLUMN IF EXISTS pin_locked_until,
    DROP COLUMN IF EXISTS pin_set_at;
