CREATE TABLE artisan_review_replies (
    id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id    UUID        NOT NULL REFERENCES artisan_reviews(id) ON DELETE CASCADE,
    author_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    author_role  VARCHAR(20) NOT NULL CHECK (author_role IN ('client', 'artisan')),
    body         TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT review_replies_unique UNIQUE (review_id, author_role)
);

CREATE INDEX idx_review_replies_review_id ON artisan_review_replies(review_id);