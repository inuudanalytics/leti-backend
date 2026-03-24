CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    transaction_type VARCHAR(10) NOT NULL CHECK (transaction_type IN ('debit', 'credit')),
    reference TEXT UNIQUE NOT NULL,
    amount NUMERIC(12, 2) NOT NULL,
    description VARCHAR(255),
    status TEXT NOT NULL CHECK (status IN ('pending', 'success', 'failed')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_transactions_user_id ON transactions(user_id);
CREATE INDEX idx_transactions_reference ON transactions(reference);
CREATE INDEX idx_transactions_status ON transactions(status);