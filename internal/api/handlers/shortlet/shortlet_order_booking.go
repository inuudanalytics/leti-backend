package shortlet

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
	"leti_server/internal/models/shortlet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderResponse struct {
	Status  string                `json:"status"`
	Message string                `json:"message"`
	Order   shortlet.Order        `json:"order"`
	Summary shortlet.OrderSummary `json:"summary"`
}

type OrderDetailResponse struct {
	Status string         `json:"status"`
	Data   shortlet.Order `json:"data"`
}

type OrderListResponse struct {
	Status     string           `json:"status"`
	Count      int              `json:"count"`
	Data       []shortlet.Order `json:"data"`
	Pagination map[string]int   `json:"pagination"`
}

type OrderPreviewResponse struct {
	Status       string                `json:"status"`
	Available    bool                  `json:"available"`
	Summary      shortlet.OrderSummary `json:"summary"`
	CheckInTime  string                `json:"check_in_time"`
	CheckOutTime string                `json:"check_out_time"`
}

// ============================================================================
// POST /orders/preview  — booking summary before payment
// ============================================================================

// PreviewOrder godoc
// @Summary      Preview booking cost breakdown
// @Description  Calculates and returns a full cost breakdown for a potential booking WITHOUT creating the order or charging the client. Use this to display a booking summary screen before the client confirms. Also validates date availability.
// @Tags         Orders
// @Accept       json
// @Produce      json
// @Param        body  body  object{property_id=string,check_in_date=string,check_out_date=string,num_adults=integer,num_children=integer}  true  "Preview request"
// @Success 200  {object} OrderPreviewResponse
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /orders/preview [post]
// @Security     BearerAuth
func PreviewOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "client" {
		utils.WriteError(w, "only clients can preview bookings", http.StatusForbidden)
		return
	}

	type request struct {
		PropertyID   string `json:"property_id"`
		CheckInDate  string `json:"check_in_date"`
		CheckOutDate string `json:"check_out_date"`
		NumAdults    int    `json:"num_adults"`
		NumChildren  int    `json:"num_children"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	propID, err := uuid.Parse(req.PropertyID)
	if err != nil {
		utils.WriteError(w, "invalid property_id", http.StatusBadRequest)
		return
	}

	checkIn, checkOut, numNights, validationErr := handlers.ValidateOrderDates(req.CheckInDate, req.CheckOutDate)
	if validationErr != "" {
		utils.WriteError(w, validationErr, http.StatusBadRequest)
		return
	}
	if req.NumAdults < 1 {
		req.NumAdults = 1
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	prop, checkInTime, checkOutTime, err := handlers.FetchPropertyForOrder(ctx, db, propID)
	if err != nil {
		utils.WriteError(w, "property not found", http.StatusNotFound)
		return
	}
	if req.NumAdults > prop.MaxAdults {
		utils.WriteError(w, fmt.Sprintf("property supports max %d adults", prop.MaxAdults), http.StatusBadRequest)
		return
	}
	if req.NumChildren > prop.MaxChildren {
		utils.WriteError(w, fmt.Sprintf("property supports max %d children", prop.MaxChildren), http.StatusBadRequest)
		return
	}

	available, err := handlers.CheckDateAvailability(ctx, db, propID, checkIn, checkOut)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	platformFeePct := handlers.FetchPlatformFeePct(ctx, db)
	summary := handlers.CalculateOrderSummary(prop.PricePerNight, prop.CautionFee, numNights, platformFeePct)

	utils.WriteJSON(w, map[string]interface{}{
		"status":         "success",
		"available":      available,
		"summary":        summary,
		"check_in_time":  checkInTime,
		"check_out_time": checkOutTime,
		"property": map[string]interface{}{
			"id":              prop.ID,
			"name":            prop.Name,
			"price_per_night": prop.PricePerNight,
			"caution_fee":     prop.CautionFee,
			"max_adults":      prop.MaxAdults,
			"max_children":    prop.MaxChildren,
		},
	})
}

// ============================================================================
// POST /orders
// ============================================================================

// CreateOrder godoc
// @Summary      Create a booking (order)
// @Description  Creates a shortlet booking for a client. The order is created with status='pending' and payment_status='pending'. Payment is collected immediately via wallet or Paystack. On successful payment the order is automatically confirmed. Double-booking is prevented with a transaction-level lock.
// @Tags         Orders
// @Accept       json
// @Produce      json
// @Param        body  body  object{property_id=string,check_in_date=string,check_out_date=string,num_adults=integer,num_children=integer,payment_method=string,email=string}  true  "payment_method: wallet | paystack; email only needed for Paystack if account has no email"
// @Success 201  {object} OrderResponse
// @Failure      400  {object}  object{error=string}
// @Failure      402  {object}  object{error=string,code=string}
// @Failure      409  {object}  object{error=string}
// @Router       /orders [post]
// @Security     BearerAuth
func CreateOrder(w http.ResponseWriter, r *http.Request) {
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
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "client" {
		utils.WriteError(w, "only clients can create orders", http.StatusForbidden)
		return
	}

	type request struct {
		PropertyID    string `json:"property_id"`
		CheckInDate   string `json:"check_in_date"`
		CheckOutDate  string `json:"check_out_date"`
		NumAdults     int    `json:"num_adults"`
		NumChildren   int    `json:"num_children"`
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

	propID, err := uuid.Parse(req.PropertyID)
	if err != nil {
		utils.WriteError(w, "invalid property_id", http.StatusBadRequest)
		return
	}

	if req.PaymentMethod != "wallet" && req.PaymentMethod != "paystack" {
		utils.WriteError(w, "payment_method must be 'wallet' or 'paystack'", http.StatusBadRequest)
		return
	}

	checkIn, checkOut, numNights, validationErr := handlers.ValidateOrderDates(req.CheckInDate, req.CheckOutDate)
	if validationErr != "" {
		utils.WriteError(w, validationErr, http.StatusBadRequest)
		return
	}
	if req.NumAdults < 1 {
		req.NumAdults = 1
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	prop, _, _, err := handlers.FetchPropertyForOrder(ctx, db, propID)
	if err != nil {
		utils.WriteError(w, "property not found or not available for booking", http.StatusNotFound)
		return
	}

	if req.NumAdults > prop.MaxAdults {
		utils.WriteError(w, fmt.Sprintf("property supports max %d adults", prop.MaxAdults), http.StatusBadRequest)
		return
	}
	if req.NumChildren > prop.MaxChildren {
		utils.WriteError(w, fmt.Sprintf("property supports max %d children", prop.MaxChildren), http.StatusBadRequest)
		return
	}

	platformFeePct := handlers.FetchPlatformFeePct(ctx, db)
	summary := handlers.CalculateOrderSummary(prop.PricePerNight, prop.CautionFee, numNights, platformFeePct)
	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var lockDummy int
	tx.QueryRow(ctx, `SELECT 1 FROM properties WHERE id = $1 FOR UPDATE`, propID).Scan(&lockDummy)

	available, err := handlers.CheckDateAvailabilityTx(ctx, tx, propID, checkIn, checkOut)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !available {
		utils.WriteError(w, "selected dates are not available — please choose different dates", http.StatusConflict)
		return
	}

	var order shortlet.Order
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (
			property_id, client_id, owner_id,
			check_in_date, check_out_date, num_nights,
			num_adults, num_children,
			price_per_night, caution_fee,
			platform_fee_pct, subtotal, platform_fee_amount, total_amount,
			status, payment_status
		) VALUES (
			$1, $2, $3,
			$4, $5, $6,
			$7, $8,
			$9, $10,
			$11, $12, $13, $14,
			'pending', 'pending'
		)
		RETURNING id, property_id, client_id, owner_id,
		          check_in_date::TEXT, check_out_date::TEXT, num_nights,
		          num_adults, num_children,
		          price_per_night, caution_fee,
		          platform_fee_pct, subtotal, platform_fee_amount, total_amount,
		          status, payment_status, created_at, updated_at
	`,
		propID, clientID, prop.OwnerID,
		req.CheckInDate, req.CheckOutDate, numNights,
		req.NumAdults, req.NumChildren,
		prop.PricePerNight, prop.CautionFee,
		platformFeePct, summary.Subtotal, summary.PlatformFeeAmount, summary.TotalAmount,
	).Scan(
		&order.ID, &order.PropertyID, &order.ClientID, &order.OwnerID,
		&order.CheckInDate, &order.CheckOutDate, &order.NumNights,
		&order.NumAdults, &order.NumChildren,
		&order.PricePerNight, &order.CautionFee,
		&order.PlatformFeePct, &order.Subtotal, &order.PlatformFeeAmount, &order.TotalAmount,
		&order.Status, &order.PaymentStatus,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to insert order: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	switch req.PaymentMethod {
	case "wallet":
		processOrderWalletPayment(w, r.WithContext(ctx), db, clientID, order, summary)
	case "paystack":
		initiateOrderPaystackPayment(w, r.WithContext(ctx), db, clientID, order, req.Email)
	}
}

// ============================================================================
// Wallet payment for order
// ============================================================================
func processOrderWalletPayment(
	w http.ResponseWriter,
	r *http.Request,
	db *pgxpool.Pool,
	clientID uuid.UUID,
	order shortlet.Order,
	summary shortlet.OrderSummary,
) {
	ctx := r.Context()
	amount := summary.TotalAmount
	commission := summary.PlatformFeeAmount
	netPayout := amount - commission - order.CautionFee // caution fee held separately

	var walletID uuid.UUID
	var balance float64
	err := db.QueryRow(ctx, `SELECT id, balance FROM wallets WHERE user_id = $1 AND is_active = TRUE`, clientID).Scan(&walletID, &balance)
	if err != nil {
		utils.WriteError(w, "wallet not found", http.StatusNotFound)
		return
	}
	if balance < amount {
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "error",
			"code":      "INSUFFICIENT_BALANCE",
			"message":   fmt.Sprintf("insufficient wallet balance. You need ₦%.2f but have ₦%.2f.", amount, balance),
			"required":  amount,
			"available": balance,
		})
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	// 1. Debit client wallet
	tx.Exec(ctx, `UPDATE wallets SET balance = balance - $1, last_transaction_at = NOW() WHERE id = $2`, amount, walletID)

	// 2. Create escrow
	var escrowID uuid.UUID
	tx.QueryRow(ctx, `
		INSERT INTO order_escrow (order_id, payer_id, payee_id, amount, commission, net_payout, status, payment_method)
		VALUES ($1, $2, $3, $4, $5, $6, 'held', 'wallet')
		RETURNING id
	`, order.ID, clientID, order.OwnerID, amount, commission, netPayout).Scan(&escrowID)

	txRef := fmt.Sprintf("ORDER-ESCROW-%s", escrowID)

	// 3. Wallet transaction log
	tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id) VALUES ($1, $2, 'escrow_hold', $3)`, walletID, amount, escrowID)

	// 4. Transactions table
	tx.Exec(ctx, `
		INSERT INTO transactions (user_id, transaction_type, reference, amount, description, status)
		VALUES ($1, 'debit', $2, $3, $4, 'success')
	`, clientID, txRef, amount, fmt.Sprintf("Escrow hold for order %s", order.ID))

	// 5. Confirm order
	var confirmedAt time.Time
	tx.QueryRow(ctx, `
		UPDATE orders SET status = 'confirmed', payment_status = 'paid',
		    payment_method = 'wallet', payment_reference = $1,
		    confirmed_at = NOW(), updated_at = NOW()
		WHERE id = $2
		RETURNING confirmed_at
	`, txRef, order.ID).Scan(&confirmedAt)

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	order.Status = "confirmed"
	order.PaymentStatus = "paid"
	t := "wallet"
	order.PaymentMethod = &t
	order.PaymentReference = &txRef
	now := time.Now()
	order.ConfirmedAt = &now

	go handlers.SendOrderConfirmationNotifications(order, db)
	go handlers.SendOrderReceipt(order, db, summary, "wallet")

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "booking confirmed — payment held in escrow",
		"order":   order,
		"summary": summary,
	})
}

// ============================================================================
// Paystack payment for order
// ============================================================================
func initiateOrderPaystackPayment(
	w http.ResponseWriter,
	r *http.Request,
	db *pgxpool.Pool,
	clientID uuid.UUID,
	order shortlet.Order,
	providedEmail string,
) {
	ctx := r.Context()

	var dbEmail sql.NullString
	db.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, clientID).Scan(&dbEmail)

	email := ""
	if dbEmail.Valid && dbEmail.String != "" {
		email = dbEmail.String
	} else if providedEmail != "" {
		if !strings.Contains(providedEmail, "@") {
			utils.WriteError(w, "invalid email address", http.StatusBadRequest)
			return
		}
		email = providedEmail
	} else {
		utils.WriteError(w, "no email found — provide one to pay with Paystack", http.StatusBadRequest)
		return
	}

	ps, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	amountKobo := int64(order.TotalAmount * 100)
	form := map[string]interface{}{
		"email":  email,
		"amount": amountKobo,
		"metadata": map[string]interface{}{
			"user_id":          clientID.String(),
			"order_id":         order.ID.String(),
			"owner_id":         order.OwnerID.String(),
			"transaction_type": "order_payment",
		},
	}

	res, err := ps.InitializePayment(form)
	if err != nil {
		utils.Logger.Errorf("Paystack init failed for order %s: %v", order.ID, err)
		utils.WriteError(w, fmt.Sprintf("failed to initialize payment: %v", err), http.StatusBadRequest)
		return
	}

	data, _ := res.Data.(map[string]interface{})
	reference, _ := data["reference"].(string)
	if reference != "" {
		db.Exec(ctx, `UPDATE orders SET payment_method = 'paystack', payment_reference = $1, updated_at = NOW() WHERE id = $2`, reference, order.ID)
	}

	utils.WriteJSON(w, res)
}

// ============================================================================
// PATCH /orders/{id}/cancel
// ============================================================================

// CancelOrder godoc
// @Summary      Cancel an order
// @Description  Cancels a pending or confirmed order. The client or owner can cancel. If payment was made, the full amount (including caution fee) is refunded to the client's wallet via escrow reversal. Orders with status checked_in or completed cannot be cancelled.
// @Tags         Orders
// @Produce      json
// @Param        id  path  string  true  "Order UUID"
// @Success      200  {object}  object{status=string,message=string,refund_processed=bool}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /orders/{id}/cancel [patch]
// @Security     BearerAuth
func CancelOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var clientID, ownerID uuid.UUID
	var paymentStatus string

	err = tx.QueryRow(ctx, `
		UPDATE orders
		SET status = 'cancelled', cancelled_at = NOW(), cancelled_by = $2, updated_at = NOW()
		WHERE id = $1
		  AND (client_id = $2 OR owner_id = $2)
		  AND status IN ('pending', 'confirmed')
		RETURNING client_id, owner_id, payment_status
	`, orderID, userID).Scan(&clientID, &ownerID, &paymentStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found or cannot be cancelled in its current state", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	refundProcessed := false
	if paymentStatus == "paid" {
		if err := handlers.RefundOrderEscrow(ctx, tx, orderID, clientID, ownerID); err != nil {
			utils.Logger.Errorf("order escrow refund failed for %s: %v", orderID, err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		refundProcessed = true
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go func() {
		bgCtx := context.Background()
		notifyID := ownerID
		if userID == ownerID {
			notifyID = clientID
		}
		utils.CreateNotification(bgCtx, notifyID,
			utils.NotifBookingCancelled,
			"Booking Cancelled",
			"A property booking has been cancelled.",
			map[string]interface{}{"order_id": orderID, "refund_processed": refundProcessed},
		)
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":           "success",
		"message":          "order cancelled",
		"refund_processed": refundProcessed,
	})
}

// ============================================================================
// PATCH /orders/{id}/check-in
// ============================================================================

// CheckInOrder godoc
// @Summary      Mark guest as checked in (owner only)
// @Description  Owner marks the guest as checked in. Order must be confirmed and check-in date must be today or earlier.
// @Tags         Orders
// @Produce      json
// @Param        id  path  string  true  "Order UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /orders/{id}/check-in [patch]
// @Security     BearerAuth
func CheckInOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "owner" {
		utils.WriteError(w, "only owners can check guests in", http.StatusForbidden)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var clientID uuid.UUID
	var checkInDateStr string
	err = db.QueryRow(ctx, `
		UPDATE orders SET status = 'checked_in', checked_in_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND owner_id = $2 AND status = 'confirmed'
		  AND check_in_date <= CURRENT_DATE
		RETURNING client_id, check_in_date::TEXT
	`, orderID, userID).Scan(&clientID, &checkInDateStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found, not confirmed, or check-in date has not arrived yet", http.StatusBadRequest)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(context.Background(), clientID,
		utils.NotifBookingCheckedIn,
		"You're Checked In! 🏠",
		"Your check-in has been confirmed. Enjoy your stay!",
		map[string]interface{}{"order_id": orderID},
	)

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "guest checked in"})
}

// ============================================================================
// PATCH /orders/{id}/check-out
// ============================================================================

// CheckOutOrder godoc
// @Summary      Mark guest as checked out — completes booking and releases escrow (owner only)
// @Description  Owner marks the guest as checked out. This completes the booking and releases the net payout (after platform commission) to the owner's wallet. The caution fee is released back to the client.
// @Tags         Orders
// @Produce      json
// @Param        id  path  string  true  "Order UUID"
// @Success      200  {object}  object{status=string,message=string,payout=number}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /orders/{id}/check-out [patch]
// @Security     BearerAuth
func CheckOutOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "owner" {
		utils.WriteError(w, "only owners can check guests out", http.StatusForbidden)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var clientID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE orders SET status = 'completed', checked_out_at = NOW(),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND owner_id = $2 AND status = 'checked_in'
		RETURNING client_id
	`, orderID, userID).Scan(&clientID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found or not in checked_in status", http.StatusBadRequest)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	netPayout, err := handlers.ReleaseOrderEscrow(ctx, tx, orderID, userID, clientID)
	if err != nil {
		utils.Logger.Errorf("order escrow release failed: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go func() {
		bgCtx := context.Background()
		utils.CreateNotification(bgCtx, clientID,
			utils.NotifBookingCheckedOut,
			"Your Stay is Complete 👋",
			"Your check-out has been confirmed. Please leave a review!",
			map[string]interface{}{"order_id": orderID},
		)
		utils.CreateNotification(bgCtx, userID,
			utils.NotifPaymentReleased,
			"Payment Released",
			fmt.Sprintf("₦%.2f has been released to your wallet.", netPayout),
			map[string]interface{}{"order_id": orderID, "amount": netPayout},
		)
		handlers.SendPushToUser(userID, "Payment Released",
			fmt.Sprintf("₦%.2f released to your wallet (after platform fee).", netPayout),
			map[string]string{"screen": "OwnerDashboard", "order_id": orderID.String()})
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "guest checked out — payment released to your wallet",
		"payout":  netPayout,
	})
}

// ============================================================================
// GET /orders  +  GET /orders/{id}
// ============================================================================

// GetMyOrders godoc
// @Summary      List orders
// @Description  Returns a paginated list of orders. Clients see orders they made; owners see orders for their properties. Filter by status optionally.
// @Tags         Orders
// @Produce      json
// @Param        status  query  string  false  "Filter by status: pending, confirmed, cancelled, checked_in, checked_out, completed, disputed"
// @Param        page    query  integer false  "Page (default 1)"
// @Param        limit   query  integer false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]shortlet.Order,pagination=object}
// @Router       /orders [get]
// @Security     BearerAuth
func GetMyOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	whereCol := "client_id"
	if role == "owner" {
		whereCol = "owner_id"
	}

	args := []interface{}{userID}
	where := whereCol + " = $1"
	argIdx := 2
	if s := r.URL.Query().Get("status"); s != "" {
		where += " AND status = $2"
		args = append(args, s)
		argIdx = 3
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var total int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE "+where, args...).Scan(&total)

	fetchArgs := append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT id, property_id, client_id, owner_id,
		       check_in_date::TEXT, check_out_date::TEXT, num_nights,
		       num_adults, num_children,
		       price_per_night, caution_fee, platform_fee_pct,
		       subtotal, platform_fee_amount, total_amount,
		       status, payment_method, payment_status, payment_reference,
		       confirmed_at, checked_in_at, checked_out_at,
		       completed_at, cancelled_at, cancelled_by,
		       created_at, updated_at
		FROM orders WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1), fetchArgs...)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	orders := make([]shortlet.Order, 0)
	for rows.Next() {
		var o shortlet.Order
		rows.Scan(
			&o.ID, &o.PropertyID, &o.ClientID, &o.OwnerID,
			&o.CheckInDate, &o.CheckOutDate, &o.NumNights,
			&o.NumAdults, &o.NumChildren,
			&o.PricePerNight, &o.CautionFee, &o.PlatformFeePct,
			&o.Subtotal, &o.PlatformFeeAmount, &o.TotalAmount,
			&o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.PaymentReference,
			&o.ConfirmedAt, &o.CheckedInAt, &o.CheckedOutAt,
			&o.CompletedAt, &o.CancelledAt, &o.CancelledBy,
			&o.CreatedAt, &o.UpdatedAt,
		)
		orders = append(orders, o)
	}

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success", "count": len(orders), "data": orders,
		"pagination": map[string]int{
			"total": total, "page": page, "limit": limit, "total_pages": totalPages,
		},
	})
}

// GetOrder godoc
// @Summary      Get a single order
// @Description  Returns full details for an order. The caller must be the client or the owner on the order.
// @Tags         Orders
// @Produce      json
// @Param        id  path  string  true  "Order UUID"
// @Success      200  {object}  object{status=string,data=shortlet.Order}
// @Failure      404  {object}  object{error=string}
// @Router       /orders/{id} [get]
// @Security     BearerAuth
func GetOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var o shortlet.Order
	err = db.QueryRow(ctx, `
		SELECT id, property_id, client_id, owner_id,
		       check_in_date::TEXT, check_out_date::TEXT, num_nights,
		       num_adults, num_children,
		       price_per_night, caution_fee, platform_fee_pct,
		       subtotal, platform_fee_amount, total_amount,
		       status, payment_method, payment_status, payment_reference,
		       confirmed_at, checked_in_at, checked_out_at,
		       completed_at, cancelled_at, cancelled_by,
		       created_at, updated_at
		FROM orders WHERE id = $1 AND (client_id = $2 OR owner_id = $2)
	`, orderID, userID).Scan(
		&o.ID, &o.PropertyID, &o.ClientID, &o.OwnerID,
		&o.CheckInDate, &o.CheckOutDate, &o.NumNights,
		&o.NumAdults, &o.NumChildren,
		&o.PricePerNight, &o.CautionFee, &o.PlatformFeePct,
		&o.Subtotal, &o.PlatformFeeAmount, &o.TotalAmount,
		&o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.PaymentReference,
		&o.ConfirmedAt, &o.CheckedInAt, &o.CheckedOutAt,
		&o.CompletedAt, &o.CancelledAt, &o.CancelledBy,
		&o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "data": o})
}
