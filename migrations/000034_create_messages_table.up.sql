CREATE TABLE IF NOT EXISTS messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       UUID        NOT NULL,
    sender_role     VARCHAR(20) NOT NULL CHECK (sender_role IN ('client', 'artisan')),
    content         TEXT        NOT NULL,
    msg_type        VARCHAR(20) NOT NULL DEFAULT 'text' CHECK (msg_type IN ('text', 'image')),
    is_read         BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fast lookup for message history (newest first)
CREATE INDEX IF NOT EXISTS idx_messages_conversation_created
    ON messages(conversation_id, created_at DESC);

-- Auto-bump conversations.updated_at on each new message
CREATE OR REPLACE FUNCTION update_conversation_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE conversations SET updated_at = NOW() WHERE id = NEW.conversation_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_update_conversation_timestamp ON messages;
CREATE TRIGGER trg_update_conversation_timestamp
    AFTER INSERT ON messages
    FOR EACH ROW EXECUTE FUNCTION update_conversation_timestamp();