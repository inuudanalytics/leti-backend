CREATE TABLE admin_audit_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id    UUID NOT NULL REFERENCES admins(id) ON DELETE SET NULL,
    action      VARCHAR(100) NOT NULL,   -- e.g. "user.suspend", "job.delete"
    entity_type VARCHAR(50),             -- e.g. "user", "job", "mechanic"
    entity_id   UUID,
    metadata    JSONB,                   -- before/after snapshot or extra context
    ip_address  VARCHAR(45),
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_admin_id    ON admin_audit_logs(admin_id);
CREATE INDEX idx_audit_entity      ON admin_audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_created_at  ON admin_audit_logs(created_at DESC);