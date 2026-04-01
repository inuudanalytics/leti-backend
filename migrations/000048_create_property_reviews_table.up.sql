CREATE TABLE property_reviews (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id UUID        NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    order_id    UUID        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    client_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating      SMALLINT    NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_review_per_order UNIQUE (order_id)
);
 
CREATE INDEX idx_prop_reviews_property ON property_reviews(property_id);
CREATE INDEX idx_prop_reviews_client   ON property_reviews(client_id);
CREATE INDEX idx_prop_reviews_rating   ON property_reviews(property_id, rating);
 
CREATE TRIGGER trg_prop_reviews_updated_at
    BEFORE UPDATE ON property_reviews
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();