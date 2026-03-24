CREATE TABLE service_variation_types (
    id          UUID           PRIMARY KEY DEFAULT uuid_generate_v4(),
    category_id UUID          NOT NULL REFERENCES job_categories(id) ON DELETE CASCADE,
    label       VARCHAR(100) NOT NULL,
    created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_variation_type_per_category UNIQUE (category_id, label)
);

CREATE INDEX idx_variation_types_category_id
    ON service_variation_types(category_id);


-- ------------------------------------------------------------
-- Seed data
-- ------------------------------------------------------------

-- -- Hairstylist
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Style type', 'Hair length', 'Hair attachment'])
-- FROM job_categories WHERE name = 'Hairstylist';

-- -- Braider
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Braid type', 'Hair length', 'Hair attachment'])
-- FROM job_categories WHERE name = 'Braider';

-- -- Barber
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Service type', 'Hair treatment'])
-- FROM job_categories WHERE name = 'Barber';

-- -- Chef
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Number of guests', 'Meal type', 'Occasion type'])
-- FROM job_categories WHERE name = 'Chef';

-- -- Painter
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Surface type', 'Number of rooms', 'Surface size'])
-- FROM job_categories WHERE name = 'Painter';

-- -- Plumber
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Job type', 'Urgency', 'Material supply'])
-- FROM job_categories WHERE name = 'Plumber';

-- -- Nanny
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Number of children', 'Duration', 'Age range'])
-- FROM job_categories WHERE name = 'Nanny';

-- -- Cleaner
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Duration', 'Property size', 'Service type'])
-- FROM job_categories WHERE name = 'Cleaner';

-- -- Electrician
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Job type', 'Urgency', 'Material supply'])
-- FROM job_categories WHERE name = 'Electrician';

-- -- DJ
-- INSERT INTO service_variation_types (category_id, label)
-- SELECT id, unnest(ARRAY['Duration', 'Event type', 'Equipment provision'])
-- FROM job_categories WHERE name = 'DJ';