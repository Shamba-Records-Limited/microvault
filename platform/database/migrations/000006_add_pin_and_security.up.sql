-- Add PIN management fields to users table
ALTER TABLE users
    ADD COLUMN pin_hash VARCHAR(72),
    ADD COLUMN pin_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN pin_locked_until TIMESTAMP,
    ADD COLUMN pin_set_at TIMESTAMP;

-- Security questions for PIN recovery
CREATE TABLE IF NOT EXISTS security_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    question_id INT NOT NULL,
    answer_hash VARCHAR(72) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (user_id, question_id)
);

CREATE INDEX idx_security_questions_user_id ON security_questions(user_id);
