CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


CREATE TABLE artisan_services (
    id          UUID           PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id  UUID           NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id UUID            NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    name        VARCHAR(100)   NOT NULL,
    description TEXT,
    base_price  NUMERIC(12, 2) NOT NULL CHECK (base_price >= 0),
    is_active   BOOLEAN        NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMP      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP      NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_artisan_service_name UNIQUE (artisan_id, category_id, name)
);

CREATE INDEX idx_artisan_services_artisan_id
    ON artisan_services(artisan_id);

CREATE INDEX idx_artisan_services_category_id
    ON artisan_services(category_id);

CREATE TRIGGER trg_artisan_services_updated_at
    BEFORE UPDATE ON artisan_services
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();