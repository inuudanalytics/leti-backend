package cronjobs

import (
	"context"
	"fmt"
	"leti_server/internal/dto"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunDailyAdCharges is called by the cron scheduler at midnight every day.
// It finds all active campaigns that haven't been charged today, attempts to
// debit the daily cost from the user's wallet, and pauses campaigns with
// insufficient balance.
func RunDailyAdCharges() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("RunDailyAdCharges: DB not initialized")
		return
	}

	today := time.Now().Format("2006-01-02")

	// Fetch every active campaign that is scheduled to run today and hasn't
	// been charged yet for today.
	rows, err := db.Query(ctx, `
		SELECT
			c.id,
			c.user_id,
			c.daily_price,
			c.mode,
			c.end_date::TEXT,
			w.id   AS wallet_id,
			w.balance
		FROM ad_campaigns c
		JOIN wallets w ON w.user_id = c.user_id AND w.is_active = TRUE
		WHERE c.status = 'active'
		  AND c.start_date <= $1::DATE
		  AND c.end_date   >= $1::DATE
		  AND (c.last_charged_date IS NULL OR c.last_charged_date < $1::DATE)
	`, today)
	if err != nil {
		utils.Logger.Errorf("RunDailyAdCharges: query error: %v", err)
		return
	}

	type campaignRow struct {
		campaignID uuid.UUID
		userID     uuid.UUID
		dailyPrice float64
		mode       string
		endDate    string
		walletID   uuid.UUID
		balance    float64
	}

	var campaigns []campaignRow
	for rows.Next() {
		var cr campaignRow
		if err := rows.Scan(
			&cr.campaignID, &cr.userID, &cr.dailyPrice,
			&cr.mode, &cr.endDate,
			&cr.walletID, &cr.balance,
		); err != nil {
			utils.Logger.Warnf("RunDailyAdCharges: scan error: %v", err)
			continue
		}
		campaigns = append(campaigns, cr)
	}
	rows.Close()

	utils.Logger.Infof("RunDailyAdCharges: processing %d campaign(s) for %s", len(campaigns), today)

	for _, cr := range campaigns {
		if err := chargeOneCampaign(ctx, db, cr.campaignID, cr.userID, cr.walletID, cr.dailyPrice, cr.mode, cr.endDate, today); err != nil {
			utils.Logger.Errorf("RunDailyAdCharges: failed for campaign %s: %v", cr.campaignID, err)
		}
	}
}

// chargeOneCampaign executes a single daily deduction transactionally.
func chargeOneCampaign(
	ctx context.Context,
	db *pgxpool.Pool,
	campaignID, userID, walletID uuid.UUID,
	dailyPrice float64,
	mode, endDate, today string,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var balance float64
	if err := tx.QueryRow(ctx,
		`SELECT balance FROM wallets WHERE id = $1 AND is_active = TRUE FOR UPDATE`, walletID,
	).Scan(&balance); err != nil {
		return fmt.Errorf("lock wallet: %w", err)
	}

	if balance < dailyPrice {
		if _, err := tx.Exec(ctx, `
			UPDATE ad_campaigns
			SET status = 'auto_paused',
			    paused_reason = 'Insufficient wallet balance for daily ad charge.',
			    updated_at = NOW()
			WHERE id = $1
		`, campaignID); err != nil {
			return fmt.Errorf("auto-pause: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO ad_daily_charges (campaign_id, user_id, wallet_id, charge_date, amount, status, failure_reason)
			VALUES ($1,$2,$3,$4,$5,'failed','Insufficient wallet balance')
			ON CONFLICT (campaign_id, charge_date) DO NOTHING
		`, campaignID, userID, walletID, today, dailyPrice); err != nil {
			utils.Logger.Warnf("chargeOneCampaign: log failed charge: %v", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit auto-pause: %w", err)
		}

		go notifyInsufficientBalance(userID, campaignID, dailyPrice, balance)
		return nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wallets SET balance = balance - $1, last_transaction_at = NOW() WHERE id = $2
	`, dailyPrice, walletID); err != nil {
		return fmt.Errorf("debit wallet: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ad_daily_charges (campaign_id, user_id, wallet_id, charge_date, amount, status)
		VALUES ($1,$2,$3,$4,$5,'success')
		ON CONFLICT (campaign_id, charge_date) DO NOTHING
	`, campaignID, userID, walletID, today, dailyPrice); err != nil {
		return fmt.Errorf("log charge: %w", err)
	}

	chargeRef := fmt.Sprintf("AD-DAILY-%s-%s", campaignID.String()[:8], today)
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1,'debit',$2,$3,$4,'success')
		ON CONFLICT (reference) DO NOTHING
	`, userID, chargeRef, dailyPrice, fmt.Sprintf("Daily ad charge for campaign %s", campaignID)); err != nil {
		utils.Logger.Warnf("chargeOneCampaign: log global tx: %v", err)
	}

	isLastDay := today == endDate
	var newStatus string
	if isLastDay {
		if mode == "recurring" {
			newStatus = "active"
		} else {
			newStatus = "completed"
		}
	} else {
		newStatus = "active"
	}

	if _, err := tx.Exec(ctx, `
		UPDATE ad_campaigns
		SET amount_spent      = amount_spent + $1,
		    last_charged_date = $2::DATE,
		    status            = $3,
		    updated_at        = NOW()
		WHERE id = $4
	`, dailyPrice, today, newStatus, campaignID); err != nil {
		return fmt.Errorf("update campaign: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit charge: %w", err)
	}

	if isLastDay && mode == "recurring" {
		go renewRecurringCampaign(db, campaignID, endDate)
	}

	go utils.CreateNotification(context.Background(), userID, utils.NotifPaymentReceived,
		"Ad Charge Processed",
		fmt.Sprintf("₦%.2f deducted from your wallet for your ad campaign today.", dailyPrice),
		map[string]interface{}{"campaign_id": campaignID, "amount": dailyPrice, "date": today},
	)

	return nil
}

// renewRecurringCampaign extends a recurring campaign by its original num_days.
func renewRecurringCampaign(db *pgxpool.Pool, campaignID uuid.UUID, oldEndDate string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var numDays int
	var userID uuid.UUID
	if err := db.QueryRow(ctx,
		`SELECT num_days, user_id FROM ad_campaigns WHERE id = $1`, campaignID,
	).Scan(&numDays, &userID); err != nil {
		utils.Logger.Errorf("renewRecurringCampaign: fetch: %v", err)
		return
	}

	end, err := time.Parse("2006-01-02", oldEndDate)
	if err != nil {
		return
	}
	newStart := end.AddDate(0, 0, 1)
	newEnd := newStart.AddDate(0, 0, numDays-1)

	if _, err := db.Exec(ctx, `
		UPDATE ad_campaigns
		SET start_date = $1, end_date = $2, status = 'active', updated_at = NOW()
		WHERE id = $3
	`, newStart.Format("2006-01-02"), newEnd.Format("2006-01-02"), campaignID); err != nil {
		utils.Logger.Errorf("renewRecurringCampaign: update: %v", err)
		return
	}

	utils.Logger.Infof("renewRecurringCampaign: renewed campaign %s → %s to %s",
		campaignID, newStart.Format("2006-01-02"), newEnd.Format("2006-01-02"))

	go utils.CreateNotification(ctx, userID, utils.NotifGeneral,
		"Campaign Renewed",
		fmt.Sprintf("Your recurring ad campaign has been renewed until %s.", newEnd.Format("2006-01-02")),
		map[string]interface{}{"campaign_id": campaignID},
	)
}

// notifyInsufficientBalance fires email, push, and SMS notifications when a
// campaign is auto-paused due to insufficient wallet balance.
func notifyInsufficientBalance(userID, campaignID uuid.UUID, required, available float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		return
	}

	_ = utils.CreateNotification(ctx, userID, utils.NotifGeneral,
		"Ad Campaign Paused — Low Balance",
		fmt.Sprintf(
			"Your ad campaign was paused because your wallet balance (₦%.2f) is insufficient for today's charge of ₦%.2f. Top up to resume.",
			available, required,
		),
		map[string]interface{}{
			"campaign_id": campaignID,
			"required":    required,
			"available":   available,
		},
	)

	dto.PushAdInsufficientBalance(userID, campaignID,
		fmt.Sprintf("%.2f", required-available))

	var email, phone, username string
	_ = db.QueryRow(ctx,
		`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &phone, &username)

	if email != "" {
		go func() {
			if err := utils.SendAdPausedEmail(email, username, required, available, campaignID.String()); err != nil {
				utils.Logger.Warnf("notifyInsufficientBalance: email failed: %v", err)
			}
		}()
	}
	if phone != "" {
		go func() {
			if err := utils.SendAdPausedSMS(phone, username, required, available); err != nil {
				utils.Logger.Warnf("notifyInsufficientBalance: SMS failed: %v", err)
			}
		}()
	}
}

// ============================================================================
// ActivatePendingCampaigns — run at midnight to activate campaigns whose
// start_date is today.
// ============================================================================
func ActivatePendingCampaigns() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		return
	}

	today := time.Now().Format("2006-01-02")

	result, err := db.Exec(ctx, `
		UPDATE ad_campaigns
		SET status = 'active', updated_at = NOW()
		WHERE status = 'pending'
		  AND payment_status = 'paid'
		  AND start_date = $1::DATE
	`, today)
	if err != nil {
		utils.Logger.Errorf("ActivatePendingCampaigns: %v", err)
		return
	}
	if n := result.RowsAffected(); n > 0 {
		utils.Logger.Infof("ActivatePendingCampaigns: activated %d campaign(s)", n)
	}
}

// ============================================================================
// ExpireCompletedCampaigns — run at midnight to mark one_time campaigns that
// have passed their end_date as completed.
// ============================================================================
func ExpireCompletedCampaigns() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		return
	}

	today := time.Now().Format("2006-01-02")

	rows, err := db.Query(ctx, `
		UPDATE ad_campaigns
		SET status = 'completed', updated_at = NOW()
		WHERE status = 'active'
		  AND end_date < $1::DATE
		  AND mode = 'one_time'
		RETURNING id, user_id
	`, today)
	if err != nil {
		utils.Logger.Errorf("ExpireCompletedCampaigns: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var campaignID, userID uuid.UUID
		if err := rows.Scan(&campaignID, &userID); err == nil {
			go utils.CreateNotification(context.Background(), userID, utils.NotifGeneral,
				"Ad Campaign Ended",
				"Your ad campaign has ended. Start a new one to keep your listing visible.",
				map[string]interface{}{"campaign_id": campaignID},
			)
		}
	}
}
