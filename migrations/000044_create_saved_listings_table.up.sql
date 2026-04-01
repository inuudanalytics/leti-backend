CREATE TABLE saved_listings (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    client_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    property_id UUID        NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_saved_listing UNIQUE (client_id, property_id)
);
 
CREATE INDEX idx_saved_listings_client_id   ON saved_listings(client_id);
CREATE INDEX idx_saved_listings_property_id ON saved_listings(property_id);
 