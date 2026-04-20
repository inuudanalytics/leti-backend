CREATE TABLE IF NOT EXISTS ad_campaigns (
    id                  UUID                PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id             UUID                NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type         ad_target_type      NOT NULL,
 
    property_id         UUID                REFERENCES properties(id) ON DELETE SET NULL,
    artisan_service_id  UUID                REFERENCES artisan_services(id) ON DELETE SET NULL,
 
    -- Ad creative
    title               VARCHAR(150)        NOT NULL,
    description         TEXT,
    image_url           TEXT                NOT NULL,
    image_public_id     TEXT                NOT NULL DEFAULT '',
 
    -- Campaign schedule
    duration_type       ad_duration_type    NOT NULL,
    num_days            SMALLINT            NOT NULL CHECK (num_days > 0),
    start_date          DATE                NOT NULL,
    end_date            DATE                NOT NULL,
    mode                ad_campaign_mode    NOT NULL DEFAULT 'one_time',
 
    -- Pricing snapshot (locked at creation time)
    daily_price         NUMERIC(12, 2)      NOT NULL CHECK (daily_price >= 0),
    total_budget        NUMERIC(12, 2)      NOT NULL CHECK (total_budget >= 0),
    amount_spent        NUMERIC(12, 2)      NOT NULL DEFAULT 0.00,
 
    payment_method      ad_payment_method   NOT NULL DEFAULT 'wallet',
    payment_status      VARCHAR(20)         NOT NULL DEFAULT 'pending'
                            CHECK (payment_status IN ('pending', 'paid', 'failed')),
    payment_reference   TEXT,
 
    status              ad_campaign_status  NOT NULL DEFAULT 'pending',
 
    total_views         BIGINT              NOT NULL DEFAULT 0,
    total_clicks        BIGINT              NOT NULL DEFAULT 0,
 
    -- Pause tracking
    paused_reason       TEXT,
    last_charged_date   DATE,               
 
    created_at          TIMESTAMPTZ         NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ         NOT NULL DEFAULT NOW(),
 
    -- A campaign must advertise exactly one thing
    CONSTRAINT chk_ad_target CHECK (
        (target_type = 'owner'   AND property_id IS NOT NULL AND artisan_service_id IS NULL)
        OR
        (target_type = 'artisan' AND artisan_service_id IS NOT NULL AND property_id IS NULL)
    ),
    CONSTRAINT chk_ad_dates CHECK (end_date >= start_date)
);
 
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_user_id    ON ad_campaigns(user_id);
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_status     ON ad_campaigns(status);
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_target     ON ad_campaigns(target_type, status);
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_dates      ON ad_campaigns(start_date, end_date);
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_property   ON ad_campaigns(property_id)
    WHERE property_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_service    ON ad_campaigns(artisan_service_id)
    WHERE artisan_service_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ad_campaigns_active_charge
    ON ad_campaigns(status, last_charged_date)
    WHERE status = 'active';
 
CREATE TRIGGER trg_ad_campaigns_updated_at
    BEFORE UPDATE ON ad_campaigns
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();