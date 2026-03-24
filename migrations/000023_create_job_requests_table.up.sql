CREATE TYPE job_request_status AS ENUM ('pending', 'accepted', 'declined');

CREATE TABLE job_requests (
    id           UUID               PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id       UUID               NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    artisan_id   UUID               NOT NULL REFERENCES users(id),
    client_id    UUID               NOT NULL REFERENCES users(id),
    status       job_request_status NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    responded_at TIMESTAMPTZ,

    UNIQUE (job_id, artisan_id)
);

CREATE INDEX idx_job_requests_artisan ON job_requests(artisan_id, status);
CREATE INDEX idx_job_requests_job      ON job_requests(job_id);