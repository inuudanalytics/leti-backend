CREATE TABLE IF NOT EXISTS ad_daily_charges (
    id              UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    campaign_id     UUID            NOT NULL REFERENCES ad_campaigns(id) ON DELETE CASCADE,
    user_id         UUID            NOT NULL REFERENCES users(id),
    wallet_id       UUID            NOT NULL REFERENCES wallets(id),
    charge_date     DATE            NOT NULL,
    amount          NUMERIC(12, 2)  NOT NULL CHECK (amount > 0),
    status          VARCHAR(20)     NOT NULL DEFAULT 'success'
                        CHECK (status IN ('success', 'failed')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_ad_charge_per_day UNIQUE (campaign_id, charge_date)
);
 
CREATE INDEX IF NOT EXISTS idx_ad_daily_charges_campaign ON ad_daily_charges(campaign_id);
CREATE INDEX IF NOT EXISTS idx_ad_daily_charges_user     ON ad_daily_charges(user_id);
CREATE INDEX IF NOT EXISTS idx_ad_daily_charges_date     ON ad_daily_charges(charge_date);