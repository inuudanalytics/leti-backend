DROP INDEX IF EXISTS idx_admin_refresh_tokens_admin_id;
DROP INDEX IF EXISTS idx_admin_refresh_tokens_token_hash;
DROP INDEX IF EXISTS idx_admin_refresh_tokens_expires_at;
DROP TABLE IF EXISTS admin_refresh_tokens;
