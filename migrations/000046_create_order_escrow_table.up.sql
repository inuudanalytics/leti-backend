CREATE TABLE order_escrow (
    id              UUID                  PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id        UUID                  NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    payer_id        UUID                  NOT NULL REFERENCES users(id),
    payee_id        UUID                  NOT NULL REFERENCES users(id),
    amount          NUMERIC(12, 2)        NOT NULL CHECK (amount > 0),
    commission      NUMERIC(12, 2)        NOT NULL DEFAULT 0.00,
    net_payout      NUMERIC(12, 2)        NOT NULL,
    status          escrow_status         NOT NULL DEFAULT 'held',
    payment_method  VARCHAR(20)           CHECK (payment_method IN ('wallet', 'paystack')),
    created_at      TIMESTAMPTZ           NOT NULL DEFAULT NOW(),
    released_at     TIMESTAMPTZ,
 
    CONSTRAINT uq_order_escrow UNIQUE (order_id)
);
 
CREATE INDEX idx_order_escrow_order_id ON order_escrow(order_id);
CREATE INDEX idx_order_escrow_status   ON order_escrow(status);
CREATE INDEX idx_order_escrow_payer    ON order_escrow(payer_id);
CREATE INDEX idx_order_escrow_payee    ON order_escrow(payee_id);
 
 