CREATE TYPE escrow_status AS ENUM (
    'held',
    'released',
    'refunded',
    'disputed'
);


CREATE TABLE jobs_escrow (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    payer_id UUID NOT NULL REFERENCES users(id),
    payee_id UUID NOT NULL REFERENCES users(id),
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    commission      DECIMAL(15,2)       NOT NULL DEFAULT 0.00,
    net_payout      DECIMAL(15,2)       NOT NULL,
    status escrow_status NOT NULL DEFAULT 'held',
    payment_method VARCHAR(20) CHECK (payment_method IN ('wallet', 'paystack')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP
);

CREATE INDEX idx_jobs_escrow_job_id ON jobs_escrow(job_id);
CREATE INDEX idx_jobs_escrow_status ON jobs_escrow(status);