CREATE TABLE IF NOT EXISTS platform_ad_pricing (
    id              UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    target_type     ad_target_type  NOT NULL UNIQUE,
    daily_price     NUMERIC(12, 2)  NOT NULL CHECK (daily_price >= 0),
    updated_by      UUID            REFERENCES admins(id) ON DELETE SET NULL,
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);
 
INSERT INTO platform_ad_pricing (target_type, daily_price) VALUES
    ('artisan', 1000.00),
    ('owner',   1500.00)
ON CONFLICT (target_type) DO NOTHING;