package paymentwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/api/services/notifications"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/apperrors"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	webhookWorkerPool chan struct{}
	workerPoolOnce    sync.Once
)

func initWebhookWorkerPool() {
	workerPoolOnce.Do(func() {
		webhookWorkerPool = make(chan struct{}, 10)
	})
}

// ============================================================================
// PaystackWebhook — receives event, persists it, ACKs immediately, processes async
// ============================================================================
func PaystackWebhook(w http.ResponseWriter, r *http.Request) {
	initWebhookWorkerPool()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusOK)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("DB is not initialized")
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.Logger.Warn("Failed to read webhook body", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	defer r.Body.Close()

	sig := r.Header.Get("X-Paystack-Signature")
	if !utils.VerifyPaystackSignature(sig, body) {
		utils.Logger.Warn("Invalid Paystack signature")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse just enough to extract the reference and event type for storage.
	// Transfer events use transfer_code; charge events use reference — handle both.
	var partial struct {
		Event string `json:"event"`
		Data  struct {
			Reference    string `json:"reference"`
			TransferCode string `json:"transfer_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		utils.Logger.Error("Invalid webhook payload", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	ref := partial.Data.Reference
	if ref == "" {
		ref = partial.Data.TransferCode
	}

	_, err = db.Exec(context.Background(), `
		INSERT INTO webhook_events (provider, event_type, reference, payload, status)
		VALUES ('paystack', $1, $2, $3, 'pending')
		ON CONFLICT (reference) DO NOTHING
	`, partial.Event, ref, body)
	if err != nil {
		utils.Logger.Error("Failed to persist webhook event", "reference", ref, "error", err)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))

	go func() {
		select {
		case webhookWorkerPool <- struct{}{}:
			defer func() { <-webhookWorkerPool }()
			processWebhookEvent(db, ref)
		default:
			utils.Logger.Warn("Worker pool full — webhook will be picked up by retry worker",
				"reference", ref)
		}
	}()
}

// ============================================================================
// processWebhookEvent — idempotent, lock-guarded processing
// ============================================================================
func processWebhookEvent(db *pgxpool.Pool, reference string) {
	ctx := context.Background()

	// Acquire a processing lock so concurrent workers don't double-process
	lockUntil := time.Now().Add(10 * time.Minute)
	result, err := db.Exec(ctx, `
		UPDATE webhook_events
		SET status = 'processing', locked_until = $2
		WHERE reference = $1
		  AND status IN ('pending', 'failed')
		  AND (locked_until IS NULL OR locked_until < now())
	`, reference, lockUntil)
	if err != nil {
		utils.Logger.Error("Failed to lock webhook", "reference", reference, "error", err)
		return
	}
	if result.RowsAffected() == 0 {
		utils.Logger.Debug("Webhook already locked or processed", "reference", reference)
		return
	}

	var payloadBytes []byte
	err = db.QueryRow(ctx, `
		SELECT payload FROM webhook_events WHERE reference = $1
	`, reference).Scan(&payloadBytes)
	if err != nil {
		utils.Logger.Error("Failed to fetch webhook payload", "reference", reference, "error", err)
		markWebhookFailed(db, reference, fmt.Sprintf("failed to fetch payload: %v", err))
		return
	}

	// Unified payload shape — handles both charge and transfer events
	var payload struct {
		Event string `json:"event"`
		Data  struct {
			Reference       string `json:"reference"`
			TransferCode    string `json:"transfer_code"`
			Amount          int    `json:"amount"`
			GatewayResponse string `json:"gateway_response"`
			Customer        struct {
				Email string `json:"email"`
			} `json:"customer"`
			Metadata map[string]interface{} `json:"metadata"`
			Status   string                 `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		markWebhookFailed(db, reference, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	var processingErr error

	switch payload.Event {

	// ── Wallet top-up  /  job payment  /  booking payment  (Paystack checkout) ─
	case "charge.success":
		if payload.Data.Status != "success" {
			markWebhookIgnored(db, reference)
			return
		}

		transactionType, ok := payload.Data.Metadata["transaction_type"].(string)
		if !ok {
			markWebhookFailed(db, reference, "transaction_type not found in metadata")
			return
		}

		userIDStr, ok := payload.Data.Metadata["user_id"].(string)
		if !ok {
			markWebhookFailed(db, reference, "user_id not found in metadata")
			return
		}
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			markWebhookFailed(db, reference, fmt.Sprintf("invalid user_id: %v", err))
			return
		}

		// Paystack sends amount in kobo — convert to Naira
		amountNaira := payload.Data.Amount / 100

		procCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		tx, err := db.Begin(procCtx)
		if err != nil {
			markWebhookFailed(db, reference, fmt.Sprintf("failed to start transaction: %v", err))
			return
		}
		defer tx.Rollback(procCtx)

		switch transactionType {
		case "credit":
			// Wallet top-up initiated from the app's fund-wallet flow
			processingErr = handleWalletCredit(procCtx, tx, db, userID, amountNaira, reference)

		case "job_payment":
			// Client paying for an artisan job (direct job flow — not a booking)
			processingErr = handleJobPayment(procCtx, tx, payload.Data.Metadata, amountNaira, reference)

		case "booking_payment":
			// Client paying for a confirmed artisan booking
			processingErr = handleBookingPayment(procCtx, tx, payload.Data.Metadata, amountNaira, reference)

		case "order_payment":
			// Shortlet order payment — stub until that flow is built
			processingErr = handleOrderPayment(procCtx, tx, payload.Data.Metadata, amountNaira, reference)

		default:
			markWebhookFailed(db, reference, fmt.Sprintf("unknown transaction_type: %s", transactionType))
			return
		}

		if processingErr != nil {
			markWebhookFailed(db, reference, processingErr.Error())
			return
		}

		if err := tx.Commit(procCtx); err != nil {
			markWebhookFailed(db, reference, fmt.Sprintf("failed to commit: %v", err))
			return
		}

	// ── Withdrawal transfer succeeded ─────────────────────────────────────────
	case "transfer.success":
		transferRef := payload.Data.Reference
		if transferRef == "" {
			transferRef = payload.Data.TransferCode
		}
		processingErr = handleTransferSuccess(ctx, db, transferRef, payload.Data.TransferCode)

	// ── Withdrawal transfer failed or reversed ────────────────────────────────
	case "transfer.failed", "transfer.reversed":
		transferRef := payload.Data.Reference
		if transferRef == "" {
			transferRef = payload.Data.TransferCode
		}
		processingErr = handleTransferFailed(ctx, db, transferRef, payload.Data.TransferCode,
			"Your withdrawal could not be processed and has been refunded to your wallet.")

	default:
		markWebhookIgnored(db, reference)
		return
	}

	if processingErr != nil {
		markWebhookFailed(db, reference, processingErr.Error())
		return
	}

	_, _ = db.Exec(ctx, `
		UPDATE webhook_events
		SET status = 'processed', processed_at = now(), locked_until = NULL
		WHERE reference = $1
	`, reference)

	utils.Logger.Info("Webhook processed successfully", "reference", reference)
}

// ============================================================================
// handleWalletCredit — Paystack wallet top-up
// ============================================================================
func handleWalletCredit(
	ctx context.Context,
	tx pgx.Tx,
	db *pgxpool.Pool,
	userID uuid.UUID,
	amountNaira int,
	reference string,
) error {
	// Idempotency check
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM transactions WHERE reference = $1 AND status = 'success')
	`, reference).Scan(&exists); err != nil {
		return fmt.Errorf("idempotency check failed: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $1, last_transaction_at = now()
		WHERE user_id = $2 AND is_active = TRUE
	`, amountNaira, userID); err != nil {
		return fmt.Errorf("failed to credit wallet: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'credit', $2, $3, 'Wallet top-up', 'success')
	`, userID, reference, amountNaira); err != nil {
		return fmt.Errorf("failed to insert transaction: %w", err)
	}

	// Push notification — fetch tokens outside the tx (read-only, non-critical)
	rows, err := db.Query(ctx, `SELECT fcm_token FROM user_devices WHERE user_id = $1`, userID)
	if err != nil {
		utils.Logger.Warn("Failed to fetch FCM tokens for wallet credit", "user_id", userID, "error", err)
		return nil
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		tokens = append(tokens, t)
	}

	go func(tokens []string) {
		for _, token := range tokens {
			if err := notifications.SendPushNotification(token,
				"Wallet Funded",
				fmt.Sprintf("Your wallet has been credited with ₦%d.", amountNaira),
			); err != nil {
				utils.Logger.Warn("Failed to send push", "user_id", userID, "error", err)
				if apperrors.IsInvalidOrExpiredTokenError(err) {
					_, _ = db.Exec(context.Background(),
						`DELETE FROM user_devices WHERE fcm_token = $1`, token)
					utils.Logger.Info("Removed expired FCM token", "user_id", userID)
				}
			}
		}
	}(tokens)

	return nil
}

// ============================================================================
// handleJobPayment — Paystack payment for a direct artisan job (non-booking)
// ============================================================================
func handleJobPayment(
	ctx context.Context,
	tx pgx.Tx,
	metadata map[string]interface{},
	amountNaira int,
	reference string,
) error {
	userIDStr, _ := metadata["user_id"].(string)
	jobIDStr, _ := metadata["job_id"].(string)

	if userIDStr == "" || jobIDStr == "" {
		return fmt.Errorf("job_payment: user_id or job_id missing in metadata")
	}

	clientID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("job_payment: invalid user_id: %v", err)
	}
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return fmt.Errorf("job_payment: invalid job_id: %v", err)
	}

	// Idempotency
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM jobs_escrow WHERE job_id = $1 AND payment_method = 'paystack')
	`, jobID).Scan(&exists); err != nil {
		return fmt.Errorf("job_payment: idempotency check failed: %w", err)
	}
	if exists {
		return nil
	}

	var artisanID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT assigned_artisan_id FROM jobs WHERE id = $1
	`, jobID).Scan(&artisanID); err != nil {
		return fmt.Errorf("job_payment: failed to fetch job: %w", err)
	}

	amount := float64(amountNaira)
	commission := amount * 0.08
	netPayout := amount - commission

	var escrowID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO jobs_escrow (job_id, payer_id, payee_id, amount, commission, net_payout, status, payment_method)
		VALUES ($1, $2, $3, $4, $5, $6, 'held', 'paystack')
		RETURNING id
	`, jobID, clientID, artisanID, amount, commission, netPayout).Scan(&escrowID); err != nil {
		return fmt.Errorf("job_payment: failed to create escrow: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'debit', $2, $3, $4, 'success')
	`, clientID, reference, amount, fmt.Sprintf("Escrow hold for job %s via Paystack", jobID)); err != nil {
		return fmt.Errorf("job_payment: failed to record transaction: %w", err)
	}

	var jobStatus string
	if err := tx.QueryRow(ctx, `
		UPDATE jobs
		SET payment_status = 'paid', payment_method = 'paystack', payment_reference = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING status
	`, reference, jobID).Scan(&jobStatus); err != nil {
		return fmt.Errorf("job_payment: failed to update job: %w", err)
	}

	// Ensure commission record exists
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_commissions (job_id, artisan_id, amount, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (job_id) DO NOTHING
	`, jobID, artisanID, commission); err != nil {
		return fmt.Errorf("job_payment: failed to upsert commission: %w", err)
	}

	go func(aID uuid.UUID, amt float64) {
		tokens, _ := handlers.GetUserFCMTokens(aID)
		for _, token := range tokens {
			notifications.SendPushNotification(token,
				"Payment Secured",
				fmt.Sprintf("₦%.2f secured in escrow via Paystack for your job.", amt),
			)
		}
	}(artisanID, amount)

	return nil
}

// ============================================================================
// handleBookingPayment — Paystack payment for a confirmed artisan booking
// ============================================================================
// Metadata shape set by initiateBookingPaystackPayment:
//
//	{
//	  "user_id":          "<client UUID>",
//	  "booking_id":       "<booking UUID>",
//	  "artisan_id":       "<artisan UUID>",
//	  "transaction_type": "booking_payment"
//	}
func handleBookingPayment(
	ctx context.Context,
	tx pgx.Tx,
	metadata map[string]interface{},
	amountNaira int,
	reference string,
) error {
	userIDStr, _ := metadata["user_id"].(string)
	bookingIDStr, _ := metadata["booking_id"].(string)
	artisanIDStr, _ := metadata["artisan_id"].(string)

	if userIDStr == "" || bookingIDStr == "" || artisanIDStr == "" {
		return fmt.Errorf("booking_payment: user_id, booking_id, or artisan_id missing in metadata")
	}

	clientID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("booking_payment: invalid user_id: %v", err)
	}
	bookingID, err := uuid.Parse(bookingIDStr)
	if err != nil {
		return fmt.Errorf("booking_payment: invalid booking_id: %v", err)
	}
	artisanID, err := uuid.Parse(artisanIDStr)
	if err != nil {
		return fmt.Errorf("booking_payment: invalid artisan_id: %v", err)
	}

	// Idempotency
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM booking_escrow
			WHERE booking_id = $1 AND payment_method = 'paystack'
		)
	`, bookingID).Scan(&exists); err != nil {
		return fmt.Errorf("booking_payment: idempotency check failed: %w", err)
	}
	if exists {
		return nil
	}

	amount := float64(amountNaira)
	commission := amount * 0.08
	netPayout := amount - commission

	var escrowID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO booking_escrow (
			booking_id, payer_id, payee_id,
			amount, commission, net_payout,
			status, payment_method
		) VALUES ($1, $2, $3, $4, $5, $6, 'held', 'paystack')
		RETURNING id
	`, bookingID, clientID, artisanID, amount, commission, netPayout).Scan(&escrowID); err != nil {
		return fmt.Errorf("booking_payment: failed to create escrow: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'debit', $2, $3, $4, 'success')
	`, clientID, reference, amount,
		fmt.Sprintf("Escrow hold for booking %s via Paystack", bookingID)); err != nil {
		return fmt.Errorf("booking_payment: failed to record transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE artisan_bookings
		SET payment_status = 'paid',
		    payment_method = 'paystack',
		    payment_reference = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, reference, bookingID); err != nil {
		return fmt.Errorf("booking_payment: failed to update booking: %w", err)
	}

	// Ensure commission record exists
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_commissions (booking_id, artisan_id, amount, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (booking_id) DO NOTHING
	`, bookingID, artisanID, commission); err != nil {
		return fmt.Errorf("booking_payment: failed to upsert commission: %w", err)
	}

	go func(aID uuid.UUID, amt float64) {
		tokens, _ := handlers.GetUserFCMTokens(aID)
		for _, token := range tokens {
			notifications.SendPushNotification(token,
				"Payment Secured",
				fmt.Sprintf("₦%.2f secured in escrow via Paystack for your booking.", amt),
			)
		}
	}(artisanID, amount)

	return nil
}

// ============================================================================
// handleOrderPayment — stub for shortlet order payments
// ============================================================================
func handleOrderPayment(
	ctx context.Context,
	tx pgx.Tx,
	metadata map[string]interface{},
	amountNaira int,
	reference string,
) error {
	// TODO: implement shortlet order escrow flow.
	// Expected metadata: user_id, order_id, owner_id, transaction_type="order_payment"
	return fmt.Errorf("order_payment: not yet implemented")
}

// ============================================================================
// handleTransferSuccess — marks withdrawal successful, notifies user
// ============================================================================
func handleTransferSuccess(ctx context.Context, db *pgxpool.Pool, transferRef, transferCode string) error {
	var userID uuid.UUID
	err := db.QueryRow(ctx, `
		UPDATE withdrawals
		SET status = 'successful',
		    transfer_code = COALESCE(NULLIF($1, ''), transfer_code),
		    completed_at  = NOW()
		WHERE (transfer_reference = $2 OR transfer_code = $1)
		  AND status = 'processing'
		RETURNING user_id
	`, transferCode, transferRef).Scan(&userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("no matching processing withdrawal: transfer_ref=%s transfer_code=%s",
				transferRef, transferCode)
		}
		return fmt.Errorf("failed to mark withdrawal successful: %w", err)
	}

	go func() {
		tokens, _ := handlers.GetUserFCMTokens(userID)
		for _, token := range tokens {
			if err := notifications.SendPushNotification(token,
				"Withdrawal Successful",
				"Your withdrawal has been sent to your bank account.",
			); err != nil {
				utils.Logger.Warn("Failed to send push", "user_id", userID, "error", err)
			}
		}
	}()

	utils.Logger.Info("Withdrawal marked successful",
		"transfer_ref", transferRef, "transfer_code", transferCode, "user_id", userID)
	return nil
}

// ============================================================================
// handleTransferFailed — marks withdrawal failed, refunds wallet, notifies user
// ============================================================================
func handleTransferFailed(ctx context.Context, db *pgxpool.Pool, transferRef, transferCode, reason string) error {
	var withdrawalID, walletID, userID uuid.UUID
	var totalDeduction float64

	err := db.QueryRow(ctx, `
		SELECT id, user_id, wallet_id, amount + fee
		FROM withdrawals
		WHERE (transfer_reference = $1 OR transfer_code = $2)
		  AND status = 'processing'
	`, transferRef, transferCode).Scan(&withdrawalID, &userID, &walletID, &totalDeduction)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("no matching processing withdrawal: transfer_ref=%s transfer_code=%s",
				transferRef, transferCode)
		}
		return fmt.Errorf("failed to fetch withdrawal for refund: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin refund tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE withdrawals SET status = 'failed', failure_reason = $1, completed_at = NOW()
		WHERE id = $2
	`, reason, withdrawalID); err != nil {
		return fmt.Errorf("failed to mark withdrawal failed: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW()
		WHERE id = $2
	`, totalDeduction, walletID); err != nil {
		return fmt.Errorf("failed to refund wallet: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
		VALUES ($1, $2, 'refund', $3)
	`, walletID, totalDeduction, withdrawalID); err != nil {
		return fmt.Errorf("failed to record refund wallet transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit withdrawal refund: %w", err)
	}

	utils.Logger.Info("Withdrawal refunded after transfer failure",
		"withdrawal_id", withdrawalID, "user_id", userID, "refund_amount", totalDeduction)

	go func() {
		tokens, _ := handlers.GetUserFCMTokens(userID)
		for _, token := range tokens {
			if err := notifications.SendPushNotification(token,
				"Withdrawal Failed",
				"Your withdrawal could not be processed and has been refunded to your wallet.",
			); err != nil {
				utils.Logger.Warn("Failed to send push", "user_id", userID, "error", err)
			}
		}
	}()

	return nil
}

// ============================================================================
// markWebhookFailed / markWebhookIgnored
// ============================================================================
func markWebhookFailed(db *pgxpool.Pool, reference, errorMsg string) {
	_, _ = db.Exec(context.Background(), `
		UPDATE webhook_events
		SET status      = 'failed',
		    error       = $2,
		    retry_count = retry_count + 1,
		    locked_until = NULL
		WHERE reference = $1
	`, reference, errorMsg)
	utils.Logger.Error("Webhook processing failed", "reference", reference, "error", errorMsg)
}

func markWebhookIgnored(db *pgxpool.Pool, reference string) {
	_, _ = db.Exec(context.Background(), `
		UPDATE webhook_events SET status = 'ignored', processed_at = now()
		WHERE reference = $1
	`, reference)
}

// ============================================================================
// StartWebhookRetryWorker — polls every 2 minutes for failed webhooks
// ============================================================================
func StartWebhookRetryWorker(ctx context.Context, db *pgxpool.Pool) {
	initWebhookWorkerPool()

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	utils.Logger.Info("Webhook retry worker started")

	for {
		select {
		case <-ctx.Done():
			utils.Logger.Info("Webhook retry worker shutting down")
			return
		case <-ticker.C:
			retryFailedWebhooks(db)
		}
	}
}

func retryFailedWebhooks(db *pgxpool.Pool) {
	ctx := context.Background()

	rows, err := db.Query(ctx, `
		SELECT reference
		FROM webhook_events
		WHERE status = 'failed'
		  AND retry_count < max_retries
		  AND created_at > now() - interval '24 hours'
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY created_at ASC
		LIMIT 10
	`)
	if err != nil {
		utils.Logger.Error("Failed to fetch retry webhooks", "error", err)
		return
	}
	defer rows.Close()

	var references []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			continue
		}
		references = append(references, ref)
	}

	for _, ref := range references {
		select {
		case webhookWorkerPool <- struct{}{}:
			go func(reference string) {
				defer func() { <-webhookWorkerPool }()
				utils.Logger.Info("Retrying webhook", "reference", reference)
				processWebhookEvent(db, reference)
			}(ref)
		default:
			utils.Logger.Warn("Worker pool full — skipping retry batch")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
