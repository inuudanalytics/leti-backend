CREATE TYPE ad_event_type AS ENUM ('view', 'click');
 
CREATE TABLE IF NOT EXISTS ad_events (
    id              UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    campaign_id     UUID            NOT NULL REFERENCES ad_campaigns(id) ON DELETE CASCADE,
    viewer_id       UUID            REFERENCES users(id) ON DELETE SET NULL, 
    event_type      ad_event_type   NOT NULL,
    ip_address      VARCHAR(45),
    user_agent      TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);
 
CREATE INDEX IF NOT EXISTS idx_ad_events_campaign_type ON ad_events(campaign_id, event_type);
CREATE INDEX IF NOT EXISTS idx_ad_events_created_at    ON ad_events(created_at DESC);
 
CREATE OR REPLACE FUNCTION increment_ad_campaign_counter()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.event_type = 'view' THEN
        UPDATE ad_campaigns SET total_views = total_views + 1 WHERE id = NEW.campaign_id;
    ELSIF NEW.event_type = 'click' THEN
        UPDATE ad_campaigns SET total_clicks = total_clicks + 1 WHERE id = NEW.campaign_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
 
DROP TRIGGER IF EXISTS trg_increment_ad_counters ON ad_events;
CREATE TRIGGER trg_increment_ad_counters
    AFTER INSERT ON ad_events
    FOR EACH ROW EXECUTE FUNCTION increment_ad_campaign_counter();