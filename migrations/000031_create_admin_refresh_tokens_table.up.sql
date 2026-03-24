CREATE TABLE admin_refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id    UUID NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_hash  VARCHAR(64) NOT NULL UNIQUE,
    device_type VARCHAR(20),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ DEFAULT NULL
);

CREATE INDEX idx_admin_refresh_tokens_admin_id    ON admin_refresh_tokens(admin_id);
CREATE INDEX idx_admin_refresh_tokens_token_hash  ON admin_refresh_tokens(token_hash);
CREATE INDEX idx_admin_refresh_tokens_expires_at  ON admin_refresh_tokens(expires_at);