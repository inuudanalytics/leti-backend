CREATE TYPE booking_escrow_status AS ENUM (
    'held',
    'released',
    'refunded',
    'disputed'
);
 
CREATE TABLE booking_escrow (
    id             UUID                  PRIMARY KEY DEFAULT uuid_generate_v4(),
    booking_id     UUID                  NOT NULL REFERENCES artisan_bookings(id) ON DELETE CASCADE,
    payer_id       UUID                  NOT NULL REFERENCES users(id), 
    payee_id       UUID                  NOT NULL REFERENCES users(id),  
    amount         DECIMAL(15, 2)        NOT NULL CHECK (amount > 0),
    commission     DECIMAL(15, 2)        NOT NULL DEFAULT 0.00,
    net_payout     DECIMAL(15, 2)        NOT NULL,
    status         booking_escrow_status NOT NULL DEFAULT 'held',
    payment_method VARCHAR(20)           CHECK (payment_method IN ('wallet', 'paystack')),
    created_at     TIMESTAMP             NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at    TIMESTAMP,
 
    CONSTRAINT uq_booking_escrow_booking UNIQUE (booking_id)
);
 
CREATE INDEX idx_booking_escrow_booking_id ON booking_escrow(booking_id);
CREATE INDEX idx_booking_escrow_status     ON booking_escrow(status);
CREATE INDEX idx_booking_escrow_payer      ON booking_escrow(payer_id);
CREATE INDEX idx_booking_escrow_payee      ON booking_escrow(payee_id);