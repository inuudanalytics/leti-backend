CREATE TABLE IF NOT EXISTS shortlet_messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES shortlet_conversations(id) ON DELETE CASCADE,
    sender_id       UUID        NOT NULL,
    sender_role     VARCHAR(20) NOT NULL CHECK (sender_role IN ('client', 'owner')),
    content         TEXT        NOT NULL,
    msg_type        VARCHAR(20) NOT NULL DEFAULT 'text' CHECK (msg_type IN ('text', 'image')),
    is_read         BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
 
CREATE INDEX IF NOT EXISTS idx_shortlet_messages_conversation_created
    ON shortlet_messages(conversation_id, created_at DESC);
 
DROP TRIGGER IF EXISTS trg_update_shortlet_conversation_timestamp ON shortlet_messages;
CREATE TRIGGER trg_update_shortlet_conversation_timestamp
    AFTER INSERT ON shortlet_messages
    FOR EACH ROW EXECUTE FUNCTION update_shortlet_conversation_timestamp();