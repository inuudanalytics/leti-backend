CREATE TABLE artisan_categories (
    id          UUID      PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id  UUID      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id UUID       NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_artisan_category UNIQUE (artisan_id, category_id)
);

CREATE INDEX idx_artisan_categories_artisan_id
    ON artisan_categories(artisan_id);

CREATE OR REPLACE FUNCTION check_artisan_category_limit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (
        SELECT COUNT(*)
        FROM artisan_categories
        WHERE artisan_id = NEW.artisan_id
    ) >= 2 THEN
        RAISE EXCEPTION
            'Artisan % already has 2 service categories. Remove one before adding another.',
            NEW.artisan_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_artisan_category_limit
    BEFORE INSERT ON artisan_categories
    FOR EACH ROW EXECUTE FUNCTION check_artisan_category_limit();



CREATE TABLE artisan_portfolio_images (
    id          UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    artisan_id  UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id UUID          NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    image_url   TEXT         NOT NULL,
    public_id   TEXT NOT NULL DEFAULT '',
    caption     VARCHAR(200),
    sort_order  SMALLINT     NOT NULL DEFAULT 0,
    created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_artisan_portfolio_category
        FOREIGN KEY (artisan_id, category_id)
        REFERENCES artisan_categories(artisan_id, category_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_portfolio_images_artisan_id
    ON artisan_portfolio_images(artisan_id);

CREATE INDEX idx_portfolio_images_artisan_category
    ON artisan_portfolio_images(artisan_id, category_id);

CREATE OR REPLACE FUNCTION check_portfolio_image_limit()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (
        SELECT COUNT(*)
        FROM artisan_portfolio_images
        WHERE artisan_id  = NEW.artisan_id
          AND category_id = NEW.category_id
    ) >= 5 THEN
        RAISE EXCEPTION
            'Artisan % already has 6 portfolio images for category %. Remove one before uploading another.',
            NEW.artisan_id,
            NEW.category_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_portfolio_image_limit
    BEFORE INSERT ON artisan_portfolio_images
    FOR EACH ROW EXECUTE FUNCTION check_portfolio_image_limit();