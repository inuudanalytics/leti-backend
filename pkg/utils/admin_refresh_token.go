package utils

import (
	"context"
	"os"
	"strconv"
	"time"

	"leti_server/internal/repositories/sqlconnect"

	"github.com/google/uuid"
)

func StoreAdminRefreshToken(ctx context.Context, adminID uuid.UUID, token, deviceType string) error {
	db := sqlconnect.DB
	if db == nil {
		return nil
	}

	durationDays, err := strconv.Atoi(os.Getenv("REFRESH_TOKEN_EXP_DAYS"))
	if err != nil {
		durationDays = 30
	}

	expiresAt := time.Now().Add(time.Duration(durationDays) * 24 * time.Hour)
	hash := HashRefreshToken(token)

	_, err = db.Exec(ctx, `
        INSERT INTO admin_refresh_tokens (admin_id, token_hash, device_type, expires_at)
        VALUES ($1, $2, $3, $4)
    `, adminID, hash, deviceType, expiresAt)
	return err
}

func RevokeAdminRefreshToken(ctx context.Context, tokenHash string) error {
	db := sqlconnect.DB
	_, err := db.Exec(ctx, `
        UPDATE admin_refresh_tokens SET revoked_at = NOW()
        WHERE token_hash = $1 AND revoked_at IS NULL
    `, tokenHash)
	return err
}

func RevokeAllAdminRefreshTokens(ctx context.Context, adminID uuid.UUID) error {
	db := sqlconnect.DB
	_, err := db.Exec(ctx, `
        UPDATE admin_refresh_tokens SET revoked_at = NOW()
        WHERE admin_id = $1 AND revoked_at IS NULL
    `, adminID)
	return err
}
