CREATE TYPE job_status AS ENUM (
    'pending',
    'accepted',
    'en_route',
    'arrived',
    'in_progress',
    'completed',
    'cancelled',
    'rejected',
    'disputed'
);

CREATE TABLE jobs (
    id                  UUID           PRIMARY KEY DEFAULT uuid_generate_v4(),
    client_id           UUID           NOT NULL REFERENCES users(id),
    category_id         UUID            NOT NULL REFERENCES job_categories(id),
    artisan_service_id  UUID           REFERENCES artisan_services(id) ON DELETE RESTRICT,
    service_option_id   UUID           REFERENCES artisan_service_options(id) ON DELETE RESTRICT,
    description         TEXT,
    images              JSONB,
    total_price         NUMERIC(12, 2) CHECK (total_price >= 0),
    status              job_status     NOT NULL DEFAULT 'pending',
    assigned_artisan_id UUID           REFERENCES users(id),
    payment_method      VARCHAR(20)    CHECK (payment_method IN ('wallet', 'paystack')),
    payment_status      VARCHAR(20)    NOT NULL DEFAULT 'pending'
                                       CHECK (payment_status IN ('pending', 'paid', 'failed')),
    payment_reference   TEXT,
    chat_expires_at     TIMESTAMP,
    completed_at        TIMESTAMP,
    confirmed_at        TIMESTAMP,
    confirmation_deadline TIMESTAMP,
    created_at          TIMESTAMP      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP      NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT job_description_check CHECK (description IS NULL OR length(description) > 0),
    CONSTRAINT job_images_check CHECK (
        images IS NULL OR (
            jsonb_typeof(images) = 'array'
            AND jsonb_array_length(images) BETWEEN 1 AND 2
        )
    )
);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER jobs_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_jobs_client_id ON jobs(client_id);
CREATE INDEX idx_jobs_assigned_artisan_id ON jobs(assigned_artisan_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_client_status ON jobs(client_id, status);
CREATE INDEX idx_jobs_artisan_status ON jobs(assigned_artisan_id, status);
CREATE INDEX idx_jobs_payment_status ON jobs(payment_status);
CREATE INDEX idx_jobs_category_id ON jobs(category_id);