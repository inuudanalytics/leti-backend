CREATE TABLE IF NOT EXISTS order_checkin_reminders (
    order_id   UUID        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    tier       VARCHAR(20) NOT NULL,  -- 'day_2' | 'day_1' | 'day_0'
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (order_id, tier)
);

CREATE INDEX IF NOT EXISTS idx_order_checkin_reminders_order_id
    ON order_checkin_reminders(order_id);