CREATE TABLE IF NOT EXISTS shortlet_conversation_archives (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    client_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    messages_json   JSONB       NOT NULL,
    archived_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_shortlet_archive_order UNIQUE (order_id)
);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_archives_order_id
    ON shortlet_conversation_archives(order_id);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_archives_client_id
    ON shortlet_conversation_archives(client_id);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_archives_owner_id
    ON shortlet_conversation_archives(owner_id);