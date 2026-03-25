CREATE TABLE artisan_availability (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id   UUID         NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    weekday       SMALLINT    NOT NULL CHECK (weekday BETWEEN 0 AND 6), 
    start_time    TIME        NOT NULL,
    end_time      TIME        NOT NULL,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT fk_artisan_availability_category
        FOREIGN KEY (artisan_id, category_id)
        REFERENCES artisan_categories(artisan_id, category_id)
        ON DELETE CASCADE,

    CONSTRAINT uq_availability_window
        UNIQUE (artisan_id, category_id, weekday, start_time);
 
    CONSTRAINT chk_availability_window
        CHECK (end_time > start_time)
);
 
CREATE INDEX idx_availability_artisan    ON artisan_availability(artisan_id);
CREATE INDEX idx_availability_category   ON artisan_availability(artisan_id, category_id);
CREATE INDEX idx_availability_weekday    ON artisan_availability(artisan_id, weekday) WHERE is_active = TRUE;
 
CREATE TRIGGER trg_artisan_availability_updated_at
    BEFORE UPDATE ON artisan_availability
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();