package cronjobs

import (
	"context"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/robfig/cron/v3"
)

func StartCronJobs() *cron.Cron {
	c := cron.New()

	// Runs once a day at midnight
	c.AddFunc("0 0 * * *", deleteExpiredUnverifiedUsers)
	c.AddFunc("0 0 * * *", cleanupRefreshTokens)

	c.Start()
	utils.Logger.Info("delete expired user accounts/clean refresh tokens jobs started")
	return c
}

func deleteExpiredUnverifiedUsers() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := sqlconnect.DB.Exec(ctx, `
		DELETE FROM users
		WHERE email_verified = FALSE
		  AND phone_verified = FALSE
		  AND user_created_at < NOW() - INTERVAL '7 days'
	`)
	if err != nil {
		utils.Logger.Errorf("cleanup cron: failed to delete unverified users: %v", err)
		return
	}

	if n := result.RowsAffected(); n > 0 {
		utils.Logger.Infof("cleanup cron: removed %d unverified account(s) older than 7 days", n)
	}
}

func cleanupRefreshTokens() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := sqlconnect.DB.Exec(ctx, `
		DELETE FROM refresh_tokens
		WHERE expires_at < NOW() OR revoked_at IS NOT NULL
	`)
	if err != nil {
		utils.Logger.Errorf("cleanup cron: failed to delete expired refresh tokens: %v", err)
		return
	}

	if n := result.RowsAffected(); n > 0 {
		utils.Logger.Infof("cleanup cron: removed %d expired/revoked refresh token(s)", n)
	}
}
