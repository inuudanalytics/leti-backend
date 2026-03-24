CREATE TYPE wallet_transactions_type AS ENUM (
    'credit',
    'debit',
    'escrow_hold',
    'escrow_release',
    'withdrawal',
    'refund'
);


CREATE TABLE wallet_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_id UUID NOT NULL 
        REFERENCES wallets(id) ON DELETE RESTRICT, 
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    type wallet_transactions_type NOT NULL, 
    reference_id UUID NOT NULL, -- job_id, booking escrow id or jobs escrow id
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_wallet_tx_wallet_id ON wallet_transactions(wallet_id);