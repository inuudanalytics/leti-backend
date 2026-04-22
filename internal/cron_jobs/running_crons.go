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

	// Midnight daily — general cleanup
	c.AddFunc("0 0 * * *", deleteExpiredUnverifiedUsers)
	c.AddFunc("0 0 * * *", cleanupRefreshTokens)

	// Hourly — archive artisan booking conversations that have expired
	c.AddFunc("0 * * * *", archiveExpiredBookingConversations)

	// Hourly — set chat_expires_at on shortlet conversations whose order
	// has just completed, and archive conversations that have already expired
	c.AddFunc("0 * * * *", setShortletChatExpiry)
	c.AddFunc("0 * * * *", archiveExpiredShortletConversations)

	// Check-in reminders: runs daily at 09:00.
	// Fires for every confirmed order whose check-in is 0, 1, or 2 days away,
	// sending each tier (day_2, day_1, day_0) at most once per order.
	c.AddFunc("0 9 * * *", sendCheckinReminders)

	// Midnight: activate campaigns whose start_date is today
	c.AddFunc("0 0 * * *", ActivatePendingCampaigns)

	// 00:05: charge active campaigns' daily fee (5 min after midnight)
	c.AddFunc("5 0 * * *", RunDailyAdCharges)

	// 00:10: mark one_time campaigns whose end_date has passed as completed
	c.AddFunc("10 0 * * *", ExpireCompletedCampaigns)

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

func archiveExpiredBookingConversations() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("archiveExpiredBookingConversations: DB not initialised")
		return
	}

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

	_, err = tx.Exec(ctx, `
		UPDATE conversations SET deleted_at = NOW() WHERE id = $1
	`, convoID)
	if err != nil {
		return fmt.Errorf("soft-delete conversation: %w", err)
	}

	return tx.Commit(ctx)
}

// ============================================================================
// Shortlet chat expiry
// ============================================================================

func setShortletChatExpiry() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		return
	}

	result, err := db.Exec(ctx, `
		UPDATE shortlet_conversations sc
		SET    chat_expires_at = o.completed_at + INTERVAL '24 hours'
		FROM   orders o
		WHERE  sc.order_id        = o.id
		  AND  o.status           = 'completed'
		  AND  o.completed_at     IS NOT NULL
		  AND  sc.chat_expires_at IS NULL
		  AND  sc.deleted_at      IS NULL
	`)
	if err != nil {
		utils.Logger.Errorf("setShortletChatExpiry: %v", err)
		return
	}

	if n := result.RowsAffected(); n > 0 {
		utils.Logger.Infof("setShortletChatExpiry: stamped expiry on %d conversation(s)", n)
	}
}

// ============================================================================
// Shortlet chat archiving
// ============================================================================

func archiveExpiredShortletConversations() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("archiveExpiredShortletConversations: DB not initialised")
		return
	}

	rows, err := db.Query(ctx, `
		SELECT id, client_id, owner_id, order_id
		FROM   shortlet_conversations
		WHERE  chat_expires_at IS NOT NULL
		  AND  chat_expires_at <= NOW()
		  AND  deleted_at IS NULL
	`)
	if err != nil {
		utils.Logger.Errorf("archiveExpiredShortletConversations: query failed: %v", err)
		return
	}
	defer rows.Close()

	type row struct {
		id       uuid.UUID
		clientID uuid.UUID
		ownerID  uuid.UUID
		orderID  uuid.UUID
	}

	var convos []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.clientID, &r.ownerID, &r.orderID); err != nil {
			utils.Logger.Warnf("archiveExpiredShortletConversations: scan error: %v", err)
			continue
		}
		convos = append(convos, r)
	}
	rows.Close()

	for _, c := range convos {
		if err := archiveSingleShortletConversation(ctx, db, c.id, c.clientID, c.ownerID, c.orderID); err != nil {
			utils.Logger.Errorf("failed to archive shortlet conversation %s: %v", c.id, err)
		}
	}

	if len(convos) > 0 {
		utils.Logger.Infof("archiveExpiredShortletConversations: archived %d conversation(s)", len(convos))
	}
}

func archiveSingleShortletConversation(
	ctx context.Context,
	db *pgxpool.Pool,
	convoID, clientID, ownerID, orderID uuid.UUID,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	msgRows, err := tx.Query(ctx, `
		SELECT id, sender_id, sender_role, content, msg_type, is_read, created_at
		FROM   shortlet_messages
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
			utils.Logger.Warnf("archiveSingleShortletConversation: scan msg: %v", err)
			continue
		}
		msgs = append(msgs, m)
	}
	msgRows.Close()

	messagesJSON, err := json.Marshal(msgs)
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO shortlet_conversation_archives
			(order_id, client_id, owner_id, messages_json)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (order_id) DO UPDATE
			SET messages_json = EXCLUDED.messages_json,
			    archived_at   = NOW()
	`, orderID, clientID, ownerID, messagesJSON)
	if err != nil {
		return fmt.Errorf("insert archive: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE shortlet_conversations SET deleted_at = NOW() WHERE id = $1
	`, convoID)
	if err != nil {
		return fmt.Errorf("soft-delete conversation: %w", err)
	}

	return tx.Commit(ctx)
}

// ============================================================================
// Check-in reminder cron job
// ============================================================================

func sendCheckinReminders() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		return
	}

	type reminderJob struct {
		orderID     uuid.UUID
		clientID    uuid.UUID
		propName    string
		checkInDate string
		checkInTime string
		clientEmail *string
		clientPhone *string
		clientName  string
		tier        string
		daysAway    int
	}

	rows, err := db.Query(ctx, `
		SELECT
			o.id,
			o.client_id,
			p.name                                      AS prop_name,
			o.check_in_date::TEXT,
			o.check_in_time::TEXT,
			u.email,
			u.phone_number,
			u.first_name || ' ' || u.last_name          AS client_name,
			t.tier,
			t.days_away
		FROM orders o
		JOIN properties p ON p.id = o.property_id
		JOIN users      u ON u.id = o.client_id
 
		CROSS JOIN LATERAL (
			VALUES
				('day_2', 2),
				('day_1', 1),
				('day_0', 0)
		) AS t(tier, days_away)
 
		WHERE o.status IN ('confirmed', 'checked_in')
		  AND o.check_in_date = CURRENT_DATE + (t.days_away || ' days')::INTERVAL
		  AND NOT EXISTS (
			SELECT 1
			FROM   order_checkin_reminders r
			WHERE  r.order_id = o.id
			  AND  r.tier     = t.tier
		  )
		ORDER BY o.check_in_date, t.days_away DESC
	`)
	if err != nil {
		utils.Logger.Errorf("sendCheckinReminders: query failed: %v", err)
		return
	}
	defer rows.Close()

	var jobs []reminderJob
	for rows.Next() {
		var j reminderJob
		if err := rows.Scan(
			&j.orderID, &j.clientID,
			&j.propName, &j.checkInDate, &j.checkInTime,
			&j.clientEmail, &j.clientPhone,
			&j.clientName,
			&j.tier, &j.daysAway,
		); err != nil {
			utils.Logger.Warnf("sendCheckinReminders: scan: %v", err)
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		j := j

		_, err := db.Exec(ctx, `
			INSERT INTO order_checkin_reminders (order_id, tier)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, j.orderID, j.tier)
		if err != nil {
			utils.Logger.Warnf("sendCheckinReminders: mark tier=%s order=%s: %v",
				j.tier, j.orderID, err)
			continue
		}

		go func(j reminderJob) {
			var label string
			switch j.daysAway {
			case 0:
				label = "today"
			case 1:
				label = "tomorrow"
			default:
				label = fmt.Sprintf("in %d days", j.daysAway)
			}

			if j.clientEmail != nil && *j.clientEmail != "" {
				if err := utils.SendCheckinReminderEmail(
					*j.clientEmail,
					j.clientName,
					j.propName,
					j.checkInDate,
					j.checkInTime,
					label,
				); err != nil {
					utils.Logger.Warnf("sendCheckinReminders: email failed order=%s tier=%s: %v",
						j.orderID, j.tier, err)
				}
			}

			if j.clientPhone != nil && *j.clientPhone != "" {
				if err := utils.SendCheckinReminderSMS(
					*j.clientPhone,
					j.clientName,
					j.propName,
					j.checkInDate,
					j.checkInTime,
				); err != nil {
					utils.Logger.Warnf("sendCheckinReminders: SMS failed order=%s tier=%s: %v",
						j.orderID, j.tier, err)
				}
			}

			utils.Logger.Infof("sendCheckinReminders: sent tier=%s order=%s label=%q",
				j.orderID, j.tier, label)
		}(j)
	}

	if len(jobs) > 0 {
		utils.Logger.Infof("sendCheckinReminders: dispatched %d reminder(s)", len(jobs))
	}
}
