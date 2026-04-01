CREATE TABLE orders (
    id                  UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id         UUID            NOT NULL REFERENCES properties(id),
    client_id           UUID            NOT NULL REFERENCES users(id),
    owner_id            UUID            NOT NULL REFERENCES users(id),

    check_in_date       DATE            NOT NULL,
    check_out_date      DATE            NOT NULL,
    num_nights          SMALLINT        NOT NULL CHECK (num_nights > 0),
    num_adults          SMALLINT        NOT NULL DEFAULT 1 CHECK (num_adults >= 1),
    num_children        SMALLINT        NOT NULL DEFAULT 0 CHECK (num_children >= 0),
 
    price_per_night     NUMERIC(12, 2)  NOT NULL,
    caution_fee         NUMERIC(12, 2)  NOT NULL DEFAULT 0.00,
    platform_fee_pct    NUMERIC(5, 2)   NOT NULL DEFAULT 5.00,  -- % charged to owner
    subtotal            NUMERIC(12, 2)  NOT NULL,               
    platform_fee_amount NUMERIC(12, 2)  NOT NULL,              
    total_amount        NUMERIC(12, 2)  NOT NULL,             
 
    status              order_status    NOT NULL DEFAULT 'pending',
 
    payment_method      VARCHAR(20)     CHECK (payment_method IN ('wallet', 'paystack')),
    payment_status      VARCHAR(20)     NOT NULL DEFAULT 'pending'
            CHECK (payment_status IN ('pending', 'paid', 'failed', 'refunded')),
    payment_reference   TEXT,
 
    confirmed_at        TIMESTAMPTZ,
    checked_in_at       TIMESTAMPTZ,
    checked_out_at      TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    cancelled_by        UUID            REFERENCES users(id),
 
    receipt_sent_at     TIMESTAMPTZ,
 
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
 
    CONSTRAINT chk_order_dates CHECK (check_out_date > check_in_date)
);
 
CREATE INDEX idx_orders_property_id  ON orders(property_id);
CREATE INDEX idx_orders_client_id    ON orders(client_id, status);
CREATE INDEX idx_orders_owner_id     ON orders(owner_id, status);
CREATE INDEX idx_orders_status       ON orders(status);
CREATE INDEX idx_orders_dates        ON orders(property_id, check_in_date, check_out_date);
CREATE INDEX idx_orders_payment      ON orders(payment_status);
 
CREATE TRIGGER trg_orders_updated_at
    BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
 