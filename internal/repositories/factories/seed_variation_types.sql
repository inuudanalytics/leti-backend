-- ============================================================================
-- Seed: service_variation_types
-- ============================================================================
-- Run once after job_categories is populated.
-- These are platform-defined — artisans DO NOT create these.
-- Artisans reference them when adding options to their services.

-- 1. Hairstylist
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Style type', 'Hair length', 'Hair attachment'])
FROM job_categories WHERE name = 'Hairstylist'
ON CONFLICT (category_id, label) DO NOTHING;

-- 2. Braider
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Braid type', 'Hair length', 'Hair attachment'])
FROM job_categories WHERE name = 'Braider'
ON CONFLICT (category_id, label) DO NOTHING;

-- 3. Barber
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Service type', 'Hair treatment'])
FROM job_categories WHERE name = 'Barber'
ON CONFLICT (category_id, label) DO NOTHING;

-- 4. Chef
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Number of guests', 'Meal type', 'Occasion type'])
FROM job_categories WHERE name = 'Chef'
ON CONFLICT (category_id, label) DO NOTHING;

-- 5. Painter
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Surface type', 'Number of rooms', 'Surface size'])
FROM job_categories WHERE name = 'Painter'
ON CONFLICT (category_id, label) DO NOTHING;

-- 6. Plumber
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Job type', 'Urgency', 'Material supply'])
FROM job_categories WHERE name = 'Plumber'
ON CONFLICT (category_id, label) DO NOTHING;

-- 7. Nanny
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Number of children', 'Duration', 'Age range'])
FROM job_categories WHERE name = 'Nanny'
ON CONFLICT (category_id, label) DO NOTHING;

-- 8. Cleaner
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Duration', 'Property size', 'Service type'])
FROM job_categories WHERE name = 'Cleaner'
ON CONFLICT (category_id, label) DO NOTHING;

-- 9. Electrician
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Job type', 'Urgency', 'Material supply'])
FROM job_categories WHERE name = 'Electrician'
ON CONFLICT (category_id, label) DO NOTHING;

-- 10. DJ
INSERT INTO service_variation_types (category_id, label)
SELECT id, unnest(ARRAY['Duration', 'Event type', 'Equipment provision'])
FROM job_categories WHERE name = 'DJ'
ON CONFLICT (category_id, label) DO NOTHING;