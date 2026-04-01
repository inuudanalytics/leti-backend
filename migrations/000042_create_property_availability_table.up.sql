CREATE TABLE property_availability (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id     UUID        NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    available_from  DATE        NOT NULL,
    available_to    DATE        NOT NULL,
    check_in_time   TIME        NOT NULL DEFAULT '14:00',
    check_out_time  TIME        NOT NULL DEFAULT '11:00',
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT chk_availability_range CHECK (available_to >= available_from)
);
 
CREATE INDEX idx_prop_avail_property_id ON property_availability(property_id);
CREATE INDEX idx_prop_avail_dates       ON property_availability(property_id, available_from, available_to)
    WHERE is_active = TRUE;
 
CREATE TRIGGER trg_prop_avail_updated_at
    BEFORE UPDATE ON property_availability
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
 
 