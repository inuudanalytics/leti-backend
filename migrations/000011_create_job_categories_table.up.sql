CREATE TABLE job_categories (
    id   UUID           PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(50) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- INSERT INTO job_categories (name) VALUES
-- ('Hairstylist'),
-- ('Braider'),
-- ('Barber'),
-- ('Chef'),
-- ('Painter'),
-- ('Plumber'),
-- ('Nanny'),
-- ('Cleaner'),
-- ('Electrician'),
-- ('DJ');