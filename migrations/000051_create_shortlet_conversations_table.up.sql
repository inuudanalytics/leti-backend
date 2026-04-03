CREATE TABLE IF NOT EXISTS shortlet_conversations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id        UUID        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    chat_expires_at TIMESTAMPTZ DEFAULT NULL,
    deleted_at      TIMESTAMPTZ DEFAULT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 
    CONSTRAINT uq_shortlet_conversation_order UNIQUE (order_id)
);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_conversations_client_id
    ON shortlet_conversations(client_id);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_conversations_owner_id
    ON shortlet_conversations(owner_id);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_conversations_deleted_at
    ON shortlet_conversations(deleted_at) WHERE deleted_at IS NULL;
 
CREATE INDEX IF NOT EXISTS idx_shortlet_conversations_chat_expires_at
    ON shortlet_conversations(chat_expires_at)
    WHERE chat_expires_at IS NOT NULL;
 
CREATE OR REPLACE FUNCTION update_shortlet_conversation_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE shortlet_conversations SET updated_at = NOW() WHERE id = NEW.conversation_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
 