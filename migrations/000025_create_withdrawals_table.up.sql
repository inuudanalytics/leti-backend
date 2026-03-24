CREATE TYPE withdrawal_status AS ENUM ('pending', 'processing', 'successful', 'failed');

CREATE TABLE withdrawals (
    id                  UUID                PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id             UUID                NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    wallet_id           UUID                NOT NULL REFERENCES wallets(id),
    bank_detail_id      UUID                NOT NULL, -- references artisan_bank_details or client_bank_details
    amount              DECIMAL(15,2)       NOT NULL CHECK (amount > 0),
    fee                 DECIMAL(15,2)       NOT NULL DEFAULT 0.00,
    net_amount          DECIMAL(15,2)       NOT NULL,
    status              withdrawal_status   NOT NULL DEFAULT 'pending',
    failure_reason      TEXT,
    transfer_reference  TEXT,
    transfer_code       TEXT,
    initiated_at        TIMESTAMPTZ         NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ
);

CREATE INDEX idx_withdrawals_user_id ON withdrawals(user_id);
CREATE INDEX idx_withdrawals_status  ON withdrawals(status);