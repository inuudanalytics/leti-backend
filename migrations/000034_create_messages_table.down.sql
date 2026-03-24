DROP TRIGGER IF EXISTS trg_update_conversation_timestamp ON messages;
DROP FUNCTION IF EXISTS update_conversation_timestamp();
DROP INDEX IF EXISTS idx_messages_conversation_created;
DROP TABLE IF EXISTS messages;