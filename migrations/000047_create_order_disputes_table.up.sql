CREATE TABLE order_disputes (
    id          UUID                    PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id    UUID                    NOT NULL REFERENCES orders(id),
    filed_by    UUID                    NOT NULL REFERENCES users(id),
    reason      TEXT                    NOT NULL,
    status      order_dispute_status    NOT NULL DEFAULT 'open',
    resolved_by UUID                    REFERENCES admins(id),
    resolution  TEXT,
    created_at  TIMESTAMPTZ             NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);
 
CREATE INDEX idx_order_disputes_order_id  ON order_disputes(order_id);
CREATE INDEX idx_order_disputes_filed_by  ON order_disputes(filed_by);
CREATE INDEX idx_order_disputes_status    ON order_disputes(status);
 