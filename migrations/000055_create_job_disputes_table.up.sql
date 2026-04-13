CREATE TABLE IF NOT EXISTS job_disputes (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id        UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    filed_by      UUID        NOT NULL REFERENCES users(id),
    respondent_id UUID        REFERENCES users(id),
    reason        TEXT        NOT NULL,
    evidence      JSONB,
    status        VARCHAR(30) NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','investigating','resolved_refund','resolved_release','dismissed')),
    admin_notes   TEXT,
    resolution    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at   TIMESTAMPTZ
);
 
CREATE INDEX IF NOT EXISTS idx_job_disputes_job_id     ON job_disputes(job_id);
CREATE INDEX IF NOT EXISTS idx_job_disputes_filed_by   ON job_disputes(filed_by);
CREATE INDEX IF NOT EXISTS idx_job_disputes_status     ON job_disputes(status);