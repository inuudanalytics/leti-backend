CREATE TABLE owner_bank_details (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bank_name VARCHAR(100) NOT NULL,
    bank_code VARCHAR(10) NOT NULL, 
    account_number VARCHAR(20) NOT NULL,
    account_name VARCHAR(150) NOT NULL,  
    recipient_code VARCHAR(100),
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_owner_bank_primary ON owner_bank_details(owner_id, is_primary);