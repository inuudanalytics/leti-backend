CREATE TABLE artisan_reviews (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating          SMALLINT    NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT artisan_reviews_unique UNIQUE (artisan_id, client_id)
);

CREATE INDEX idx_artisan_reviews_artisan_id   ON artisan_reviews(artisan_id);
CREATE INDEX idx_artisan_reviews_client_id  ON artisan_reviews(client_id);
CREATE INDEX idx_artisan_reviews_rating        ON artisan_reviews(artisan_id, rating);

CREATE TRIGGER artisan_reviews_updated_at
    BEFORE UPDATE ON artisan_reviews
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();