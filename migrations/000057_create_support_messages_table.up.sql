CREATE TABLE IF NOT EXISTS support_messages (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   UUID        NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    sender_id   UUID        NOT NULL,
    sender_type VARCHAR(10) NOT NULL CHECK (sender_type IN ('user','admin')),
    content     TEXT        NOT NULL,
    msg_type    VARCHAR(10) NOT NULL DEFAULT 'text' CHECK (msg_type IN ('text','image')),
    is_read     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
 
CREATE INDEX IF NOT EXISTS idx_support_messages_ticket_id
    ON support_messages(ticket_id, created_at ASC);
 