-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mobile_number VARCHAR(20) UNIQUE NOT NULL,
    mobile_country_code VARCHAR(5) NOT NULL DEFAULT '254',
    full_name VARCHAR(255),
    national_id VARCHAR(50) UNIQUE,
    kyc_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    kyc_verified_at TIMESTAMP,
    preferred_language VARCHAR(10) NOT NULL DEFAULT 'en',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    role VARCHAR(20) NOT NULL DEFAULT 'user',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX idx_users_deleted_at ON users(deleted_at);
CREATE INDEX idx_users_kyc_status ON users(kyc_status);
CREATE INDEX idx_users_status ON users(status);

-- Create accounts table
CREATE TABLE IF NOT EXISTS accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    public_key VARCHAR(56) UNIQUE NOT NULL,
    account_index INT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_accounts_user_id ON accounts(user_id);
CREATE INDEX idx_accounts_deleted_at ON accounts(deleted_at);

-- Create transactions table
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID,
    account_id UUID,
    loan_id UUID,
    tx_type VARCHAR(50) NOT NULL,
    tx_category VARCHAR(20) NOT NULL DEFAULT 'on_chain',
    amount BIGINT NOT NULL,
    asset VARCHAR(20) NOT NULL,
    stellar_tx_hash VARCHAR(64) UNIQUE,
    stellar_ledger BIGINT,
    stellar_status VARCHAR(20),
    contract_id VARCHAR(56),
    contract_function VARCHAR(100),
    external_id VARCHAR(100),
    external_provider VARCHAR(50),
    external_status VARCHAR(20),
    description TEXT,
    metadata JSONB,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL
);

CREATE INDEX idx_transactions_user_id ON transactions(user_id);
CREATE INDEX idx_transactions_account_id ON transactions(account_id);
CREATE INDEX idx_transactions_loan_id ON transactions(loan_id);
CREATE INDEX idx_transactions_tx_type ON transactions(tx_type);
CREATE INDEX idx_transactions_tx_category ON transactions(tx_category);
CREATE INDEX idx_transactions_asset ON transactions(asset);
CREATE INDEX idx_transactions_contract_id ON transactions(contract_id);
CREATE INDEX idx_transactions_external_id ON transactions(external_id);
CREATE INDEX idx_transactions_external_provider ON transactions(external_provider);
CREATE INDEX idx_transactions_status ON transactions(status);
CREATE INDEX idx_transactions_created_at ON transactions(created_at);
