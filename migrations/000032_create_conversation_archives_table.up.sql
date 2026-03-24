CREATE TABLE IF NOT EXISTS conversation_archives (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    owner_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    artisan_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    messages_json   JSONB NOT NULL,
    archived_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for faster lookups if you ever need to retrieve archives
CREATE INDEX IF NOT EXISTS idx_conversation_archives_job_id ON conversation_archives(job_id);
CREATE INDEX IF NOT EXISTS idx_conversation_archives_owner_id ON conversation_archives(owner_id);
CREATE INDEX IF NOT EXISTS idx_conversation_archives_artisan_id ON conversation_archives(artisan_id);