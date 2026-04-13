CREATE TYPE support_ticket_status AS ENUM (
    'open',
    'assigned',
    'in_progress',
    'waiting_user',
    'resolved',
    'closed'
);
 
CREATE TYPE support_ticket_category AS ENUM (
    'payment_and_refund',
    'booking_and_reservation',
    'service_issues',
    'disputes',
    'account_and_verification',
    'technical_issue',
    'report_user_or_property',
    'general_inquiry',
    'other'
);
 
CREATE TABLE IF NOT EXISTS support_tickets (
    id              UUID                    PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID                    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_dispute_id     UUID REFERENCES job_disputes(id)     ON DELETE SET NULL,
    booking_dispute_id UUID REFERENCES booking_disputes(id) ON DELETE SET NULL,
    order_dispute_id   UUID REFERENCES order_disputes(id)   ON DELETE SET NULL,
    assigned_admin_id  UUID                  REFERENCES admins(id) ON DELETE SET NULL,
    subject         VARCHAR(255)            NOT NULL,
    category        support_ticket_category NOT NULL DEFAULT 'general_inquiry',
    status          support_ticket_status   NOT NULL DEFAULT 'open',
    priority        VARCHAR(10)             NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low','medium','high','urgent')),
    resolved_at     TIMESTAMP,
    created_at      TIMESTAMPTZ             NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ             NOT NULL DEFAULT NOW()
);
 
CREATE INDEX IF NOT EXISTS idx_support_tickets_user_id       ON support_tickets(user_id);
CREATE INDEX IF NOT EXISTS idx_support_tickets_status        ON support_tickets(status);
CREATE INDEX IF NOT EXISTS idx_support_tickets_assigned      ON support_tickets(assigned_admin_id);
CREATE INDEX IF NOT EXISTS idx_support_tickets_category      ON support_tickets(category);
CREATE INDEX IF NOT EXISTS idx_support_tickets_job_dispute_id
    ON support_tickets(job_dispute_id) WHERE job_dispute_id IS NOT NULL;
 
CREATE INDEX IF NOT EXISTS idx_support_tickets_booking_dispute_id
    ON support_tickets(booking_dispute_id) WHERE booking_dispute_id IS NOT NULL;
 
CREATE INDEX IF NOT EXISTS idx_support_tickets_order_dispute_id
    ON support_tickets(order_dispute_id) WHERE order_dispute_id IS NOT NULL;