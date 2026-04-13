CREATE TABLE IF NOT EXISTS booking_disputes (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id    UUID        NOT NULL REFERENCES artisan_bookings(id) ON DELETE CASCADE,
    filed_by      UUID        NOT NULL REFERENCES users(id),
    respondent_id UUID        NOT NULL REFERENCES users(id),
    reason        TEXT        NOT NULL,
    evidence      JSONB,
    status        VARCHAR(30) NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','investigating','resolved_refund','resolved_release','dismissed')),
    admin_notes   TEXT,
    resolution    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at   TIMESTAMPTZ
);
 
CREATE INDEX IF NOT EXISTS idx_booking_disputes_booking_id  ON booking_disputes(booking_id);
CREATE INDEX IF NOT EXISTS idx_booking_disputes_filed_by    ON booking_disputes(filed_by);
CREATE INDEX IF NOT EXISTS idx_booking_disputes_status      ON booking_disputes(status);
 