CREATE TABLE property_review_replies (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id   UUID        NOT NULL REFERENCES property_reviews(id) ON DELETE CASCADE,
    author_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    author_role VARCHAR(20) NOT NULL CHECK (author_role IN ('client', 'owner')),
    body        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_review_reply_per_role UNIQUE (review_id, author_role)
);
 
CREATE INDEX idx_prop_review_replies ON property_review_replies(review_id);
 