package cronjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
)

func StartCronJobs() *cron.Cron {
	c := cron.New()

	// Runs once a day at midnight
	c.AddFunc("0 0 * * *", deleteExpiredUnverifiedUsers)
	c.AddFunc("0 0 * * *", cleanupRefreshTokens)

	c.AddFunc("0 * * * *", archiveExpiredBookingConversations)

	c.Start()
	utils.Logger.Info("cron jobs started")
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

// archiveExpiredBookingConversations finds conversations whose chat_expires_at
// has passed, copies their messages to conversation_archives, then soft-deletes
// the conversation so the hub rejects any new writes.
func archiveExpiredBookingConversations() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("archiveExpiredBookingConversations: DB not initialised")
		return
	}

	// Fetch conversations ready to archive (expiry passed, not yet deleted)
	rows, err := db.Query(ctx, `
		SELECT id, owner_id, artisan_id, booking_id
		FROM   conversations
		WHERE  chat_expires_at IS NOT NULL
		  AND  chat_expires_at <= NOW()
		  AND  deleted_at IS NULL
	`)
	if err != nil {
		utils.Logger.Errorf("archiveExpiredBookingConversations: query failed: %v", err)
		return
	}
	defer rows.Close()

	type row struct {
		id        uuid.UUID
		ownerID   uuid.UUID
		artisanID uuid.UUID
		bookingID *uuid.UUID
	}

	var convos []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ownerID, &r.artisanID, &r.bookingID); err != nil {
			utils.Logger.Warnf("archiveExpiredBookingConversations: scan error: %v", err)
			continue
		}
		convos = append(convos, r)
	}
	rows.Close()

	for _, c := range convos {
		if err := archiveSingleConversation(ctx, db, c.id, c.ownerID, c.artisanID, c.bookingID); err != nil {
			utils.Logger.Errorf("failed to archive conversation %s: %v", c.id, err)
		}
	}

	if len(convos) > 0 {
		utils.Logger.Infof("archiveExpiredBookingConversations: archived %d conversation(s)", len(convos))
	}
}

func archiveSingleConversation(
	ctx context.Context,
	db *pgxpool.Pool,
	convoID, ownerID, artisanID uuid.UUID,
	bookingID *uuid.UUID,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Collect all messages as JSON
	msgRows, err := tx.Query(ctx, `
		SELECT id, sender_id, sender_role, content, msg_type, is_read, created_at
		FROM   messages
		WHERE  conversation_id = $1
		ORDER  BY created_at ASC
	`, convoID)
	if err != nil {
		return fmt.Errorf("fetch messages: %w", err)
	}

	type msgRecord struct {
		ID         uuid.UUID `json:"id"`
		SenderID   uuid.UUID `json:"sender_id"`
		SenderRole string    `json:"sender_role"`
		Content    string    `json:"content"`
		MsgType    string    `json:"msg_type"`
		IsRead     bool      `json:"is_read"`
		CreatedAt  time.Time `json:"created_at"`
	}

	var msgs []msgRecord
	for msgRows.Next() {
		var m msgRecord
		if err := msgRows.Scan(
			&m.ID, &m.SenderID, &m.SenderRole,
			&m.Content, &m.MsgType, &m.IsRead, &m.CreatedAt,
		); err != nil {
			utils.Logger.Warnf("archiveSingleConversation: scan msg error: %v", err)
			continue
		}
		msgs = append(msgs, m)
	}
	msgRows.Close()

	messagesJSON, err := json.Marshal(msgs)
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	// Write to conversation_archives
	// conversation_archives has job_id (NOT booking_id) — look up the job from
	// the conversations table if we only have booking_id, or store as-is.
	// Since your archive table uses job_id, resolve it here.
	var jobID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT job_id FROM conversations WHERE id = $1`, convoID,
	).Scan(&jobID)
	if err != nil {
		return fmt.Errorf("resolve job_id: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO conversation_archives
			(job_id, owner_id, artisan_id, messages_json)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (job_id) DO UPDATE
			SET messages_json = EXCLUDED.messages_json,
			    archived_at   = NOW()
	`, jobID, ownerID, artisanID, messagesJSON)
	if err != nil {
		return fmt.Errorf("insert archive: %w", err)
	}

	// Soft-delete the live conversation — hub will reject new writes
	_, err = tx.Exec(ctx, `
		UPDATE conversations SET deleted_at = NOW() WHERE id = $1
	`, convoID)
	if err != nil {
		return fmt.Errorf("soft-delete conversation: %w", err)
	}

	return tx.Commit(ctx)
}
