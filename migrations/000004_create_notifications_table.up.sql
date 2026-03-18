CREATE TYPE notification_type AS ENUM (
    -- Bookings (shortlets)
    'booking_request',
    'booking_confirmed',
    'booking_declined',
    'booking_cancelled',
    'booking_completed',
    'booking_reminder',
    'booking_checked_in',
    'booking_checked_out',

    -- Artisan jobs
    'job_request',
    'job_accepted',
    'job_declined',
    'job_cancelled',
    'job_completed',
    'job_quote_received',
    'job_quote_accepted',
    'job_quote_rejected',

    -- Payments
    'payment_received',
    'payment_released',
    'payment_held',
    'payment_refunded',
    'escrow_funded',

    -- Reviews
    'review_received',

    -- Role switching
    'role_activated',

    -- Disputes & support
    'dispute_filed',
    'dispute_resolved',
    'support_ticket_opened',
    'support_ticket_reply',
    'support_ticket_resolved',

    -- General
    'general'
);

CREATE TABLE notifications (
    id         UUID              PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID              NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       notification_type NOT NULL,
    title      VARCHAR(255)      NOT NULL,
    body       TEXT              NOT NULL,
    data       JSONB,
    is_read    BOOLEAN           NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ       NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notifications_user_id     ON notifications(user_id);
CREATE INDEX idx_notifications_user_unread ON notifications(user_id, is_read) WHERE is_read = FALSE;
CREATE INDEX idx_notifications_created_at  ON notifications(created_at DESC);