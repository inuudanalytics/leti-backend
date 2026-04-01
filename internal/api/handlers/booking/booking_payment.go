package booking

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/api/services"
	bookingModels "leti_server/internal/models/booking"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================================
// POST /bookings/{id}/pay
// ============================================================================
// Called by the client AFTER the artisan has confirmed the booking.
// Supports wallet and Paystack.
//
// Flow:
//   pending  →  artisan confirms  →  client pays here  →  payment_status = paid
//   artisan completes  →  escrow released to artisan wallet (minus commission)

// InitiateBookingPayment godoc
// @Summary      Pay for a confirmed booking
// @Description  Initiates payment for a booking that has been confirmed by the artisan. Supports wallet (instant) and Paystack (redirect). The full amount is held in escrow and only released to the artisan after the booking is marked completed. Only callable by the client on the booking.
// @Tags         Booking
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Booking UUID"
// @Param        body  body  object{payment_method=string,email=string}  true  "payment_method: wallet | paystack"
// @Success      200   {object}  object{status=string,message=string,escrow_id=string,commission=number,payout=number}
// @Failure      400   {object}  object{error=string}
// @Failure      402   {object}  object{error=string,code=string,required=number,available=number}
// @Failure      404   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /bookings/{id}/pay [post]
// @Security     BearerAuth
func InitiateBookingPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	clientID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "client" {
		utils.WriteError(w, "only clients can pay for bookings", http.StatusForbidden)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	type request struct {
		PaymentMethod string `json:"payment_method"`
		Email         string `json:"email,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.PaymentMethod != "wallet" && req.PaymentMethod != "paystack" {
		utils.WriteError(w, "payment_method must be 'wallet' or 'paystack'", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var bk bookingModels.Booking
	err = db.QueryRow(ctx, `
		SELECT id, client_id, artisan_id, total_price, status, payment_status
		FROM artisan_bookings
		WHERE id = $1
	`, bookingID).Scan(
		&bk.ID, &bk.ClientID, &bk.ArtisanID,
		&bk.TotalPrice, &bk.Status, &bk.PaymentStatus,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "booking not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch booking: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if bk.ClientID != clientID {
		utils.WriteError(w, "you do not own this booking", http.StatusForbidden)
		return
	}

	if bk.Status != "confirmed" {
		utils.WriteError(w, "payment is only allowed for confirmed bookings — wait for the artisan to accept first", http.StatusBadRequest)
		return
	}

	if bk.PaymentStatus == "paid" {
		utils.WriteError(w, "this booking has already been paid for", http.StatusConflict)
		return
	}

	commission := bk.TotalPrice * 0.08
	_, err = db.Exec(ctx, `
		INSERT INTO platform_commissions (booking_id, artisan_id, amount, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (booking_id) WHERE booking_id IS NOT NULL DO NOTHING
	`, bookingID, bk.ArtisanID, commission)
	if err != nil {
		utils.Logger.Errorf("failed to ensure commission record: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	switch req.PaymentMethod {
	case "wallet":
		processBookingWalletPayment(w, r.WithContext(ctx), db, clientID, bk, commission)
	case "paystack":
		initiateBookingPaystackPayment(w, r.WithContext(ctx), db, clientID, bk, req.Email)
	}
}

// ============================================================================
// processBookingWalletPayment — debit client wallet, hold in booking_escrow
// ============================================================================
func processBookingWalletPayment(
	w http.ResponseWriter,
	r *http.Request,
	db *pgxpool.Pool,
	clientID uuid.UUID,
	bk bookingModels.Booking,
	commission float64,
) {
	amount := bk.TotalPrice
	artisanPayout := amount - commission

	// Check wallet balance
	var walletID uuid.UUID
	var balance float64
	err := db.QueryRow(r.Context(), `
		SELECT id, balance FROM wallets WHERE user_id = $1 AND is_active = TRUE
	`, clientID).Scan(&walletID, &balance)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "wallet not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch wallet: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if balance < amount {
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "error",
			"message":   fmt.Sprintf("insufficient wallet balance. You need ₦%.2f but have ₦%.2f. Please top up your wallet.", amount, balance),
			"code":      "INSUFFICIENT_BALANCE",
			"required":  amount,
			"available": balance,
		})
		return
	}

	tx, err := db.Begin(r.Context())
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Debit client wallet
	_, err = tx.Exec(r.Context(), `
		UPDATE wallets
		SET balance = balance - $1, last_transaction_at = NOW()
		WHERE id = $2
	`, amount, walletID)
	if err != nil {
		utils.Logger.Errorf("failed to debit wallet: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 2. Create escrow (full amount held)
	var escrowID uuid.UUID
	err = tx.QueryRow(r.Context(), `
		INSERT INTO booking_escrow (
			booking_id, payer_id, payee_id,
			amount, commission, net_payout,
			status, payment_method
		) VALUES ($1, $2, $3, $4, $5, $6, 'held', 'wallet')
		RETURNING id
	`, bk.ID, clientID, bk.ArtisanID, amount, commission, artisanPayout).Scan(&escrowID)
	if err != nil {
		utils.Logger.Errorf("failed to create booking escrow: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	txRef := fmt.Sprintf("BOOKING-ESCROW-%s", escrowID.String())

	// 3. Record wallet transaction (escrow_hold)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
		VALUES ($1, $2, 'escrow_hold', $3)
	`, walletID, amount, escrowID)
	if err != nil {
		utils.Logger.Errorf("failed to record wallet transaction: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 4. Record in transactions table (debit side)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'debit', $2, $3, $4, 'success')
	`, clientID, txRef, amount,
		fmt.Sprintf("Escrow hold for booking %s", bk.ID))
	if err != nil {
		utils.Logger.Errorf("failed to record transaction: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 5. Update booking payment status
	_, err = tx.Exec(r.Context(), `
		UPDATE artisan_bookings
		SET payment_method = 'wallet', payment_status = 'paid', payment_reference = $1, updated_at = NOW()
		WHERE id = $2
	`, txRef, bk.ID)
	if err != nil {
		utils.Logger.Errorf("failed to update booking payment status: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		utils.Logger.Errorf("failed to commit wallet payment: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Notify artisan that payment is secured
	go func() {
		bgCtx := context.Background()

		utils.CreateNotification(bgCtx, bk.ArtisanID,
			utils.NotifPaymentHeld,
			"Payment Secured",
			fmt.Sprintf("Payment of ₦%.2f has been secured in escrow for your booking.", amount),
			map[string]interface{}{
				"booking_id": bk.ID,
				"escrow_id":  escrowID,
				"amount":     amount,
			},
		)

		var artisanPhone string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(phone_number, '') FROM users WHERE id = $1`, bk.ArtisanID,
		).Scan(&artisanPhone)

		handlers.SendPushToUser(bk.ArtisanID, "Payment Secured",
			fmt.Sprintf("₦%.2f secured in escrow for your booking.", amount),
			map[string]string{
				"screen":     "BookingDetails",
				"booking_id": bk.ID.String(),
				"escrow_id":  escrowID.String(),
			})
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":     "success",
		"message":    "payment completed via wallet — escrow funded",
		"escrow_id":  escrowID,
		"commission": commission,
		"payout":     artisanPayout,
	})
}

// ============================================================================
// initiateBookingPaystackPayment — returns Paystack checkout URL to the client
// ============================================================================
func initiateBookingPaystackPayment(
	w http.ResponseWriter,
	r *http.Request,
	db *pgxpool.Pool,
	clientID uuid.UUID,
	bk bookingModels.Booking,
	providedEmail string,
) {
	// Resolve email: prefer DB, fall back to provided
	var dbEmail sql.NullString
	_ = db.QueryRow(r.Context(),
		`SELECT email FROM users WHERE id = $1`, clientID,
	).Scan(&dbEmail)

	email := ""
	if dbEmail.Valid && dbEmail.String != "" {
		email = dbEmail.String
	} else if providedEmail != "" {
		if !strings.Contains(providedEmail, "@") {
			utils.WriteError(w, "invalid email address provided", http.StatusBadRequest)
			return
		}
		email = providedEmail
	} else {
		utils.WriteError(w, "no email address found — please provide an email to pay with Paystack", http.StatusBadRequest)
		return
	}

	ps, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	amountKobo := int64(bk.TotalPrice * 100)

	form := map[string]interface{}{
		"email":  email,
		"amount": amountKobo,
		"metadata": map[string]interface{}{
			"user_id":          clientID.String(),
			"booking_id":       bk.ID.String(),
			"artisan_id":       bk.ArtisanID.String(),
			"transaction_type": "booking_payment",
		},
	}

	res, err := ps.InitializePayment(form)
	if err != nil {
		utils.Logger.Errorf("Paystack init failed for booking %s: %v", bk.ID, err)
		utils.WriteError(w, fmt.Sprintf("failed to initialize payment: %v", err), http.StatusBadRequest)
		return
	}

	data, _ := res.Data.(map[string]interface{})
	reference, _ := data["reference"].(string)
	if reference != "" {
		_, _ = db.Exec(r.Context(), `
			UPDATE artisan_bookings
			SET payment_method = 'paystack', payment_reference = $1, updated_at = NOW()
			WHERE id = $2
		`, reference, bk.ID)
	}

	utils.WriteJSON(w, res)
}

// ============================================================================
// ReleaseBookingEscrow — called from CompleteBooking after artisan marks done.
// Releases net_payout to artisan wallet, marks commission paid.
// Mirrors BrodaMeko's ProcessEscrowRelease.
// ============================================================================
func ReleaseBookingEscrow(ctx context.Context, tx pgx.Tx, bookingID, artisanID uuid.UUID) error {
	var escrowID uuid.UUID
	var amount, commission, netPayout float64
	var paymentMethod string

	err := tx.QueryRow(ctx, `
		SELECT id, amount, commission, net_payout, payment_method
		FROM booking_escrow
		WHERE booking_id = $1 AND status = 'held'
	`, bookingID).Scan(&escrowID, &amount, &commission, &netPayout, &paymentMethod)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil // no escrow — nothing to release (e.g. cash or not yet paid)
		}
		return fmt.Errorf("failed to fetch booking escrow: %w", err)
	}

	// 1. Mark escrow released
	_, err = tx.Exec(ctx, `
		UPDATE booking_escrow
		SET status = 'released', released_at = NOW()
		WHERE id = $1
	`, escrowID)
	if err != nil {
		return fmt.Errorf("failed to release escrow: %w", err)
	}

	// 2. Credit artisan wallet (net payout after commission)
	var artisanWalletID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO wallets (user_id, balance, last_transaction_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE
			SET balance = wallets.balance + $2, last_transaction_at = NOW()
		RETURNING id
	`, artisanID, netPayout).Scan(&artisanWalletID)
	if err != nil {
		return fmt.Errorf("failed to credit artisan wallet: %w", err)
	}

	// 3. Record wallet transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
		VALUES ($1, $2, 'escrow_release', $3)
	`, artisanWalletID, netPayout, escrowID)
	if err != nil {
		return fmt.Errorf("failed to record artisan wallet transaction: %w", err)
	}

	// 4. Record in transactions table
	releaseRef := fmt.Sprintf("BOOKING-RELEASE-%s", escrowID.String())
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'credit', $2, $3, $4, 'success')
	`, artisanID, releaseRef, netPayout,
		fmt.Sprintf("Escrow release for booking %s (after 8%% commission)", bookingID))
	if err != nil {
		return fmt.Errorf("failed to record release transaction: %w", err)
	}

	// 5. Mark platform commission as paid
	_, err = tx.Exec(ctx, `
		UPDATE platform_commissions
		SET status = 'paid', paid_at = NOW()
		WHERE booking_id = $1 AND artisan_id = $2 AND status = 'pending'
	`, bookingID, artisanID)
	if err != nil {
		// Non-fatal — log but don't fail the release
		utils.Logger.Warnf("failed to mark commission paid for booking %s: %v", bookingID, err)
	}

	return nil
}

// ============================================================================
// RefundBookingEscrow — called from CancelBooking when payment_status = 'paid'.
// Mirrors BrodaMeko's processEscrowRefund.
// ============================================================================
func RefundBookingEscrow(ctx context.Context, tx pgx.Tx, bookingID, clientID, artisanID uuid.UUID) error {
	var escrowID uuid.UUID
	var amount float64

	err := tx.QueryRow(ctx, `
		SELECT id, amount
		FROM booking_escrow
		WHERE booking_id = $1 AND status = 'held'
	`, bookingID).Scan(&escrowID, &amount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return fmt.Errorf("failed to fetch escrow for refund: %w", err)
	}

	// 1. Mark escrow refunded
	_, err = tx.Exec(ctx, `
		UPDATE booking_escrow
		SET status = 'refunded', released_at = NOW()
		WHERE id = $1
	`, escrowID)
	if err != nil {
		return fmt.Errorf("failed to mark escrow refunded: %w", err)
	}

	// 2. Credit full amount back to client wallet
	var clientWalletID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE wallets
		SET balance = balance + $1, last_transaction_at = NOW()
		WHERE user_id = $2 AND is_active = TRUE
		RETURNING id
	`, amount, clientID).Scan(&clientWalletID)
	if err != nil {
		return fmt.Errorf("failed to refund client wallet: %w", err)
	}

	// 3. Record wallet transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
		VALUES ($1, $2, 'refund', $3)
	`, clientWalletID, amount, escrowID)
	if err != nil {
		return fmt.Errorf("failed to record refund wallet transaction: %w", err)
	}

	// 4. Record in transactions table
	refundRef := fmt.Sprintf("BOOKING-REFUND-%s", escrowID.String())
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'credit', $2, $3, $4, 'success')
	`, clientID, refundRef, amount,
		fmt.Sprintf("Escrow refund for cancelled booking %s", bookingID))
	if err != nil {
		return fmt.Errorf("failed to record refund transaction: %w", err)
	}

	// 5. Waive platform commission since booking was cancelled
	_, err = tx.Exec(ctx, `
		UPDATE platform_commissions
		SET status = 'waived', paid_at = NOW()
		WHERE booking_id = $1 AND artisan_id = $2 AND status = 'pending'
	`, bookingID, artisanID)
	if err != nil {
		utils.Logger.Warnf("failed to waive commission for cancelled booking %s: %v", bookingID, err)
	}

	return nil
}
