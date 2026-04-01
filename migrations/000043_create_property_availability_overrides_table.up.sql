CREATE TABLE property_availability_overrides (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id     UUID        NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    blocked_date    DATE        NOT NULL,
    reason          VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_property_blocked_date UNIQUE (property_id, blocked_date)
);
 
CREATE INDEX idx_prop_overrides_property ON property_availability_overrides(property_id, blocked_date);
 