CREATE TABLE artisan_availability_overrides (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id   UUID         NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    override_date DATE        NOT NULL,
    is_available  BOOLEAN     NOT NULL,
    note          VARCHAR(255),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_override UNIQUE (artisan_id, category_id, override_date),
 
    CONSTRAINT fk_artisan_override_category
        FOREIGN KEY (artisan_id, category_id)
        REFERENCES artisan_categories(artisan_id, category_id)
        ON DELETE CASCADE
);
 
CREATE INDEX idx_overrides_artisan_date
    ON artisan_availability_overrides(artisan_id, override_date);
 