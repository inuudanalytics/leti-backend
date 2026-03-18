// pkg/utils/refresh_token.go
package utils

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"time"

	"leti_server/internal/repositories/sqlconnect"

	"github.com/google/uuid"
)

func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func HashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func StoreRefreshToken(ctx context.Context, userID uuid.UUID, token, deviceType string) error {
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
		INSERT INTO refresh_tokens (user_id, token_hash, device_type, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, hash, deviceType, expiresAt)
	return err
}

func RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	db := sqlconnect.DB
	_, err := db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
}

func RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	db := sqlconnect.DB
	_, err := db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}
