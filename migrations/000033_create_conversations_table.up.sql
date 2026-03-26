CREATE TABLE IF NOT EXISTS conversations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    UUID NOT NULL,
    artisan_id  UUID NOT NULL,
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    deleted_at  TIMESTAMPTZ DEFAULT NULL,
    booking_id  UUID REFERENCES artisan_bookings(id) ON DELETE SET NULL,
    chat_expires_at TIMESTAMPTZ DEFAULT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (job_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_archives_job_id_unique
    ON conversation_archives(job_id);
CREATE INDEX idx_conversations_owner_id    ON conversations(owner_id);
CREATE INDEX idx_conversations_artisan_id ON conversations(artisan_id);
CREATE INDEX idx_conversations_deleted_at  ON conversations(deleted_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_conversations_booking_id ON conversations(booking_id);
CREATE INDEX IF NOT EXISTS idx_conversations_chat_expires_at
    ON conversations(chat_expires_at)
    WHERE chat_expires_at IS NOT NULL;