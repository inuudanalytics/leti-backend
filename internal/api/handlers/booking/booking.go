package booking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/models/booking"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// POST /bookings
// ============================================================================
// Payment is NOT collected here. The booking is created with status='pending'
// and payment_status='pending'. The client only pays after the artisan confirms.

// CreateBooking godoc
// @Summary      Request a booking
// @Description  Creates a booking request for a specific artisan, date, and time slot. The slot must fall within the artisan's active availability window and must not already be booked. The booking starts in 'pending' status — the artisan must confirm before any payment is collected. Payment method is chosen at payment time (POST /bookings/{id}/pay), not here.
// @Tags         Booking
// @Accept       json
// @Produce      json
// @Param        body  body  object{artisan_id=string,category_id=string,service_id=string,service_option_id=string,booking_date=string,start_time=string,address=string,note=string}  true  "Booking request"
// @Success      201  {object}  object{status=string,message=string,booking=booking.Booking}
// @Failure      400  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /bookings [post]
// @Security     BearerAuth
func CreateBooking(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only clients can create bookings", http.StatusForbidden)
		return
	}

	type request struct {
		ArtisanID       string    `json:"artisan_id"`
		CategoryID      uuid.UUID `json:"category_id"`
		ServiceID       *string   `json:"service_id,omitempty"`
		ServiceOptionID *string   `json:"service_option_id,omitempty"`
		BookingDate     string    `json:"booking_date"`
		StartTime       string    `json:"start_time"`
		Address         *string   `json:"address,omitempty"`
		Note            *string   `json:"note,omitempty"`
		// PaymentMethod is intentionally absent here — the client chooses their
		// payment method when they pay (POST /bookings/{id}/pay), after the
		// artisan has confirmed the booking.
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ArtisanID == "" {
		utils.WriteError(w, "artisan_id is required", http.StatusBadRequest)
		return
	}
	artisanID, err := uuid.Parse(req.ArtisanID)
	if err != nil {
		utils.WriteError(w, "invalid artisan_id", http.StatusBadRequest)
		return
	}
	if req.CategoryID == uuid.Nil {
		utils.WriteError(w, "category_id is required", http.StatusBadRequest)
		return
	}
	if req.BookingDate == "" {
		utils.WriteError(w, "booking_date is required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	parsedDate, err := time.Parse("2006-01-02", req.BookingDate)
	if err != nil {
		utils.WriteError(w, "booking_date must be in YYYY-MM-DD format", http.StatusBadRequest)
		return
	}
	if parsedDate.Before(time.Now().Truncate(24 * time.Hour)) {
		utils.WriteError(w, "booking_date cannot be in the past", http.StatusBadRequest)
		return
	}
	if req.StartTime == "" {
		utils.WriteError(w, "start_time is required (HH:MM)", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("15:04", req.StartTime); err != nil {
		utils.WriteError(w, "start_time must be in HH:MM format", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	weekday := int(parsedDate.Weekday())
	dateStr := req.BookingDate

	// ── Override check ────────────────────────────────────────────────────────
	var isAvailableOverride *bool
	ovErr := db.QueryRow(ctx, `
		SELECT is_available FROM artisan_availability_overrides
		WHERE artisan_id = $1 AND category_id = $2 AND override_date = $3
	`, artisanID, req.CategoryID, dateStr).Scan(&isAvailableOverride)
	if ovErr != nil && ovErr != pgx.ErrNoRows {
		utils.Logger.Errorf("failed to check override: %v", ovErr)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if isAvailableOverride != nil && !*isAvailableOverride {
		utils.WriteError(w, "the artisan is not available on this date", http.StatusBadRequest)
		return
	}

	// ── Find the availability window that matches start_time ──────────────────
	var endTime string
	err = db.QueryRow(ctx, `
		SELECT end_time::TEXT
		FROM artisan_availability
		WHERE artisan_id  = $1
		  AND category_id = $2
		  AND weekday     = $3
		  AND start_time  = $4::TIME
		  AND is_active   = TRUE
	`, artisanID, req.CategoryID, weekday, req.StartTime).Scan(&endTime)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "no availability window found for this artisan on the chosen date and time", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("failed to fetch availability window: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Resolve price ─────────────────────────────────────────────────────────
	var totalPrice float64
	var serviceName string = "Booking"

	if req.ServiceID != nil {
		serviceID, err := uuid.Parse(*req.ServiceID)
		if err != nil {
			utils.WriteError(w, "invalid service_id", http.StatusBadRequest)
			return
		}
		var basePrice float64
		err = db.QueryRow(ctx,
			`SELECT base_price, name FROM artisan_services WHERE id = $1 AND artisan_id = $2 AND is_active = TRUE`,
			serviceID, artisanID,
		).Scan(&basePrice, &serviceName)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "service not found or not active", http.StatusBadRequest)
				return
			}
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		totalPrice = basePrice

		if req.ServiceOptionID != nil {
			optID, err := uuid.Parse(*req.ServiceOptionID)
			if err != nil {
				utils.WriteError(w, "invalid service_option_id", http.StatusBadRequest)
				return
			}
			var modifier float64
			err = db.QueryRow(ctx,
				`SELECT price_modifier FROM artisan_service_options WHERE id = $1 AND service_id = $2`,
				optID, serviceID,
			).Scan(&modifier)
			if err != nil {
				if err == pgx.ErrNoRows {
					utils.WriteError(w, "service option not found", http.StatusBadRequest)
					return
				}
				utils.WriteError(w, "internal server error", http.StatusInternalServerError)
				return
			}
			totalPrice += modifier
		}
	}

	// ── Tx: advisory lock  →  slot conflict check  →  insert ─────────────────
	tx, err := db.Begin(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to begin tx: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var lockDummy int
	_ = tx.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, artisanID).Scan(&lockDummy)

	booked, err := handlers.SlotIsBooked(ctx, tx, artisanID, req.CategoryID, dateStr, req.StartTime)
	if err != nil {
		utils.Logger.Errorf("failed to check slot: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if booked {
		utils.WriteError(w, "this time slot is already booked", http.StatusConflict)
		return
	}

	var bk booking.Booking
	var serviceIDParam, serviceOptIDParam interface{}
	if req.ServiceID != nil {
		sid, _ := uuid.Parse(*req.ServiceID)
		serviceIDParam = sid
	}
	if req.ServiceOptionID != nil {
		soid, _ := uuid.Parse(*req.ServiceOptionID)
		serviceOptIDParam = soid
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO artisan_bookings (
			client_id, artisan_id, category_id,
			service_id, service_option_id,
			booking_date, start_time, end_time,
			total_price, address, note,
			status, payment_status
		) VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $7, $8,
			$9, $10, $11,
			'pending', 'pending'
		)
		RETURNING id, client_id, artisan_id, category_id,
		          service_id, service_option_id,
		          booking_date::TEXT, start_time::TEXT, end_time::TEXT,
		          total_price, address, note,
		          status, payment_method, payment_status,
		          created_at, updated_at
	`,
		clientID, artisanID, req.CategoryID,
		serviceIDParam, serviceOptIDParam,
		dateStr, req.StartTime, endTime,
		totalPrice, req.Address, req.Note,
	).Scan(
		&bk.ID, &bk.ClientID, &bk.ArtisanID, &bk.CategoryID,
		&bk.ServiceID, &bk.ServiceOptionID,
		&bk.BookingDate, &bk.StartTime, &bk.EndTime,
		&bk.TotalPrice, &bk.Address, &bk.Note,
		&bk.Status, &bk.PaymentMethod, &bk.PaymentStatus,
		&bk.CreatedAt, &bk.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to insert booking: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.Logger.Errorf("failed to commit booking tx: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Fetch artisan contact details for email/SMS ───────────────────────────
	go func() {
		bgCtx := context.Background()

		var artisanEmail, artisanPhone, artisanFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
			artisanID,
		).Scan(&artisanEmail, &artisanPhone, &artisanFirstName)

		var clientFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT first_name FROM users WHERE id = $1`, clientID,
		).Scan(&clientFirstName)

		// In-app notification
		utils.CreateNotification(bgCtx, artisanID,
			utils.NotifBookingRequest,
			"New Booking Request",
			"You have a new booking request. Please accept or decline.",
			map[string]interface{}{"booking_id": bk.ID},
		)

		// Email
		if artisanEmail != "" {
			utils.SendBookingRequestEmail(
				artisanEmail,
				artisanFirstName,
				clientFirstName,
				serviceName,
				bk.BookingDate,
				bk.StartTime,
			)
		}

		// SMS
		if artisanPhone != "" {
			utils.SendBookingRequestSMS(
				artisanPhone,
				artisanFirstName,
				clientFirstName,
				serviceName,
				bk.BookingDate,
				bk.StartTime,
			)
		}
	}()

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "booking request sent — awaiting artisan confirmation",
		"booking": bk,
	})
}

// ============================================================================
// PATCH /bookings/{id}/confirm
// ============================================================================
// Artisan confirms the booking.
// This does NOT collect payment — it notifies the client to pay.
// Payment is handled by a separate POST /bookings/{id}/pay endpoint.

// ConfirmBooking godoc
// @Summary      Confirm a booking (artisan only)
// @Description  Artisan confirms a pending booking. The booking moves to 'confirmed' and the client is notified to complete payment. No money is collected here.
// @Tags         Booking
// @Produce      json
// @Param        id  path  string  true  "Booking UUID"
// @Success      200  {object}  object{status=string,message=string,booking=booking.Booking}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /bookings/{id}/confirm [patch]
// @Security     BearerAuth
func ConfirmBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "artisan" {
		utils.WriteError(w, "only artisans can confirm bookings", http.StatusForbidden)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var bk booking.Booking
	err = db.QueryRow(ctx, `
		UPDATE artisan_bookings
		SET    status = 'confirmed', confirmed_at = NOW(), updated_at = NOW()
		WHERE  id = $1 AND artisan_id = $2 AND status = 'pending'
		RETURNING id, client_id, artisan_id, category_id,
		          service_id, service_option_id,
		          booking_date::TEXT, start_time::TEXT, end_time::TEXT,
		          total_price, address, note,
		          status, payment_method, payment_status,
		          confirmed_at, created_at, updated_at
	`, bookingID, userID).Scan(
		&bk.ID, &bk.ClientID, &bk.ArtisanID, &bk.CategoryID,
		&bk.ServiceID, &bk.ServiceOptionID,
		&bk.BookingDate, &bk.StartTime, &bk.EndTime,
		&bk.TotalPrice, &bk.Address, &bk.Note,
		&bk.Status, &bk.PaymentMethod, &bk.PaymentStatus,
		&bk.ConfirmedAt, &bk.CreatedAt, &bk.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "booking not found or not pending", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to confirm booking: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Notify client to pay ──────────────────────────────────────────────────
	go func() {
		bgCtx := context.Background()

		var clientEmail, clientPhone, clientFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
			bk.ClientID,
		).Scan(&clientEmail, &clientPhone, &clientFirstName)

		var artisanFirstName, serviceName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT first_name FROM users WHERE id = $1`, bk.ArtisanID,
		).Scan(&artisanFirstName)
		if bk.ServiceID != nil {
			_ = sqlconnect.DB.QueryRow(bgCtx,
				`SELECT name FROM artisan_services WHERE id = $1`, *bk.ServiceID,
			).Scan(&serviceName)
		}
		if serviceName == "" {
			serviceName = "Booking"
		}

		// In-app notification
		utils.CreateNotification(bgCtx, bk.ClientID,
			utils.NotifBookingConfirmed,
			"Booking Confirmed – Payment Required",
			"Your booking has been confirmed. Please complete your payment to secure the slot.",
			map[string]interface{}{
				"booking_id":  bk.ID,
				"total_price": bk.TotalPrice,
			},
		)

		// Email
		if clientEmail != "" {
			utils.SendBookingConfirmedEmail(
				clientEmail,
				clientFirstName,
				artisanFirstName,
				serviceName,
				bk.BookingDate,
				bk.StartTime,
				fmt.Sprintf("%.2f", bk.TotalPrice),
			)
		}

		// SMS
		if clientPhone != "" {
			utils.SendBookingConfirmedSMS(
				clientPhone,
				clientFirstName,
				artisanFirstName,
				serviceName,
				bk.BookingDate,
			)
		}
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "booking confirmed — client has been notified to complete payment",
		"booking": bk,
	})
}

// ============================================================================
// PATCH /bookings/{id}/decline
// ============================================================================

// DeclineBooking godoc
// @Summary      Decline a booking (artisan only)
// @Description  Artisan declines a pending booking. No payment was collected so no refund is needed.
// @Tags         Booking
// @Produce      json
// @Param        id  path  string  true  "Booking UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /bookings/{id}/decline [patch]
// @Security     BearerAuth
func DeclineBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "artisan" {
		utils.WriteError(w, "only artisans can decline bookings", http.StatusForbidden)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch enough data to send notifications before updating
	var clientID, artisanID uuid.UUID
	var bookingDate, serviceName string
	var serviceID *uuid.UUID

	err = db.QueryRow(ctx, `
		UPDATE artisan_bookings
		SET    status = 'declined', declined_at = NOW(), updated_at = NOW()
		WHERE  id = $1 AND artisan_id = $2 AND status = 'pending'
		RETURNING client_id, artisan_id, booking_date::TEXT, service_id
	`, bookingID, userID).Scan(&clientID, &artisanID, &bookingDate, &serviceID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "booking not found or not pending", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to decline booking: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go func() {
		bgCtx := context.Background()

		var clientEmail, clientPhone, clientFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
			clientID,
		).Scan(&clientEmail, &clientPhone, &clientFirstName)

		var artisanFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT first_name FROM users WHERE id = $1`, artisanID,
		).Scan(&artisanFirstName)

		if serviceID != nil {
			_ = sqlconnect.DB.QueryRow(bgCtx,
				`SELECT name FROM artisan_services WHERE id = $1`, *serviceID,
			).Scan(&serviceName)
		}
		if serviceName == "" {
			serviceName = "Booking"
		}

		// In-app notification
		utils.CreateNotification(bgCtx, clientID,
			utils.NotifBookingDeclined,
			"Booking Request Declined",
			"The artisan has declined your booking request.",
			map[string]interface{}{"booking_id": bookingID},
		)

		// Email
		if clientEmail != "" {
			utils.SendBookingDeclinedEmail(
				clientEmail,
				clientFirstName,
				artisanFirstName,
				serviceName,
				bookingDate,
			)
		}

		// SMS
		if clientPhone != "" {
			utils.SendBookingDeclinedSMS(
				clientPhone,
				clientFirstName,
				artisanFirstName,
				serviceName,
			)
		}
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "booking declined",
	})
}

// ============================================================================
// PATCH /bookings/{id}/cancel
// ============================================================================

// CancelBooking godoc
// @Summary      Cancel a booking
// @Description  Either the client or the artisan can cancel a booking that is pending or confirmed. If payment was already made, a refund is triggered separately via the escrow/payment service.
// @Tags         Booking
// @Produce      json
// @Param        id  path  string  true  "Booking UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /bookings/{id}/cancel [patch]
// @Security     BearerAuth
func CancelBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Use a transaction: cancellation may trigger an escrow refund atomically.
	tx, err := db.Begin(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to begin cancel tx: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var clientID, artisanID uuid.UUID
	var bookingDate, paymentStatus string
	var serviceID *uuid.UUID

	err = tx.QueryRow(ctx, `
		UPDATE artisan_bookings
		SET    status       = 'cancelled',
		       cancelled_at = NOW(),
		       cancelled_by = $2,
		       updated_at   = NOW()
		WHERE  id = $1
		  AND  (client_id = $2 OR artisan_id = $2)
		  AND  status IN ('pending', 'confirmed')
		RETURNING client_id, artisan_id, booking_date::TEXT, payment_status, service_id
	`, bookingID, userID).Scan(&clientID, &artisanID, &bookingDate, &paymentStatus, &serviceID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "booking not found or cannot be cancelled", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to cancel booking: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Determine who cancelled and who to notify
	cancelledByRole := "client"
	notifyID := artisanID
	if userID == artisanID {
		cancelledByRole = "artisan"
		notifyID = clientID
	}

	// If client already paid, refund escrow to their wallet inside the same tx.
	refundProcessed := false
	if paymentStatus == "paid" {
		if err := RefundBookingEscrow(ctx, tx, bookingID, clientID, artisanID); err != nil {
			utils.Logger.Errorf("escrow refund failed for booking %s: %v", bookingID, err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		refundProcessed = true
	}

	if err := tx.Commit(ctx); err != nil {
		utils.Logger.Errorf("failed to commit cancellation: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go func() {
		bgCtx := context.Background()

		// Fetch both parties
		var notifyEmail, notifyPhone, notifyFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
			notifyID,
		).Scan(&notifyEmail, &notifyPhone, &notifyFirstName)

		var cancellerFirstName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT first_name FROM users WHERE id = $1`, userID,
		).Scan(&cancellerFirstName)

		var serviceName string
		if serviceID != nil {
			_ = sqlconnect.DB.QueryRow(bgCtx,
				`SELECT name FROM artisan_services WHERE id = $1`, *serviceID,
			).Scan(&serviceName)
		}
		if serviceName == "" {
			serviceName = "Booking"
		}

		// In-app notification
		utils.CreateNotification(bgCtx, notifyID,
			utils.NotifBookingCancelled,
			"Booking Cancelled",
			"A booking has been cancelled.",
			map[string]interface{}{
				"booking_id":        bookingID,
				"cancelled_by_role": cancelledByRole,
				// refund_processed = true means the wallet was already credited
				"refund_processed": refundProcessed,
			},
		)

		// Email
		if notifyEmail != "" {
			utils.SendBookingCancelledEmail(
				notifyEmail,
				notifyFirstName,
				cancellerFirstName,
				serviceName,
				bookingDate,
				cancelledByRole,
			)
		}

		// SMS
		if notifyPhone != "" {
			utils.SendBookingCancelledSMS(
				notifyPhone,
				notifyFirstName,
				cancellerFirstName,
				serviceName,
				bookingDate,
				cancelledByRole,
			)
		}
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "booking cancelled",
	})
}

// ============================================================================
// PATCH /bookings/{id}/complete
// ============================================================================
// Dual-confirmation model:
//   - Artisan marks complete  → artisan_completed_at set, escrow NOT released
//   - Client marks complete   → client_completed_at set, escrow released
//   - Both have marked        → second call by either party triggers full release
//
// The conversation linked to this booking is scheduled to expire 24h after
// both parties confirm.

// CompleteBooking godoc
// @Summary      Mark a booking as completed
// @Description  Either the artisan or client can mark a booking as completed.
//
//	Escrow is only released when the CLIENT confirms — treating the
//	client's confirmation as the authoritative satisfaction signal.
//	If the artisan marks complete first, the booking moves to
//	'awaiting_client_confirmation'. If the client marks complete
//	(with or without the artisan), escrow is released immediately.
//	The linked conversation is scheduled to close 24h after full completion.
//
// @Tags         Booking
// @Produce      json
// @Param        id  path  string  true  "Booking UUID"
// @Success      200  {object}  object{status=string,message=string,booking=booking.Booking}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /bookings/{id}/complete [patch]
// @Security     BearerAuth
func CompleteBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "artisan" && role != "client" {
		utils.WriteError(w, "only artisans and clients can mark a booking as completed", http.StatusForbidden)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to begin complete tx: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var bk booking.Booking
	var clientCompletedAt, artisanCompletedAt *time.Time

	err = tx.QueryRow(ctx, `
		SELECT id, client_id, artisan_id, category_id,
		       service_id, service_option_id,
		       booking_date::TEXT, start_time::TEXT, end_time::TEXT,
		       total_price, address, note,
		       status, payment_method, payment_status,
		       confirmed_at, completed_at, created_at, updated_at,
		       client_completed_at, artisan_completed_at
		FROM artisan_bookings
		WHERE id = $1
		  AND (client_id = $2 OR artisan_id = $2)
		  AND status IN ('confirmed', 'awaiting_client_confirmation')
		  AND payment_status = 'paid'
	`, bookingID, userID).Scan(
		&bk.ID, &bk.ClientID, &bk.ArtisanID, &bk.CategoryID,
		&bk.ServiceID, &bk.ServiceOptionID,
		&bk.BookingDate, &bk.StartTime, &bk.EndTime,
		&bk.TotalPrice, &bk.Address, &bk.Note,
		&bk.Status, &bk.PaymentMethod, &bk.PaymentStatus,
		&bk.ConfirmedAt, &bk.CompletedAt, &bk.CreatedAt, &bk.UpdatedAt,
		&clientCompletedAt, &artisanCompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			var exists bool
			_ = db.QueryRow(ctx,
				`SELECT EXISTS(
					SELECT 1 FROM artisan_bookings
					WHERE id = $1 AND (client_id = $2 OR artisan_id = $2)
					  AND status = 'confirmed'
				)`, bookingID, userID,
			).Scan(&exists)
			if exists {
				utils.WriteError(w, "cannot complete booking: client payment has not been received yet", http.StatusConflict)
				return
			}
			utils.WriteError(w, "booking not found or not in a completable state", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch booking for completion: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if role == "artisan" && artisanCompletedAt != nil {
		utils.WriteError(w, "you have already marked this booking as complete", http.StatusConflict)
		return
	}
	if role == "client" && clientCompletedAt != nil {
		utils.WriteError(w, "you have already marked this booking as complete", http.StatusConflict)
		return
	}

	now := time.Now()
	fullyComplete := false
	var newStatus string
	var responseMessage string

	if role == "artisan" {
		_, err = tx.Exec(ctx, `
			UPDATE artisan_bookings
			SET artisan_completed_at = $1,
			    status               = 'awaiting_client_confirmation',
			    updated_at           = $1
			WHERE id = $2
		`, now, bookingID)
		newStatus = "awaiting_client_confirmation"
		responseMessage = "booking marked as complete — waiting for client confirmation to release payment"

	} else {
		fullyComplete = true
		_, err = tx.Exec(ctx, `
			UPDATE artisan_bookings
			SET client_completed_at = $1,
			    completed_at        = $1,
			    status              = 'completed',
			    updated_at          = $1
			WHERE id = $2
		`, now, bookingID)
		newStatus = "completed"
		responseMessage = "booking completed — payment released to artisan"
	}

	if err != nil {
		utils.Logger.Errorf("failed to update booking completion: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if fullyComplete {
		if err := ReleaseBookingEscrow(ctx, tx, bk.ID, bk.ArtisanID); err != nil {
			utils.Logger.Errorf("escrow release failed for booking %s: %v", bk.ID, err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		chatExpiresAt := now.Add(24 * time.Hour)
		_, err = tx.Exec(ctx, `
			UPDATE conversations
			SET chat_expires_at = $1
			WHERE booking_id = $2
			  AND deleted_at IS NULL
		`, chatExpiresAt, bookingID)
		if err != nil {
			utils.Logger.Warnf("failed to set chat expiry for booking %s: %v", bookingID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		utils.Logger.Errorf("failed to commit completion: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	bk.Status = newStatus

	go func(fullyComplete bool, role string, bk booking.Booking, artisanCompletedAt *time.Time) {
		bgCtx := context.Background()

		var artisanFirstName, serviceName string
		var clientEmail, clientPhone, clientFirstName string

		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT first_name FROM users WHERE id = $1`, bk.ArtisanID,
		).Scan(&artisanFirstName)
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), first_name FROM users WHERE id = $1`,
			bk.ClientID,
		).Scan(&clientEmail, &clientPhone, &clientFirstName)

		if bk.ServiceID != nil {
			_ = sqlconnect.DB.QueryRow(bgCtx,
				`SELECT name FROM artisan_services WHERE id = $1`, *bk.ServiceID,
			).Scan(&serviceName)
		}
		if serviceName == "" {
			serviceName = "Booking"
		}

		if fullyComplete {
			netPayout := bk.TotalPrice * 0.92

			utils.CreateNotification(bgCtx, bk.ClientID,
				utils.NotifBookingCompleted,
				"Booking Completed",
				"Your booking has been completed. Please leave a review.",
				map[string]interface{}{"booking_id": bk.ID},
			)
			utils.CreateNotification(bgCtx, bk.ArtisanID,
				utils.NotifPaymentReleased,
				"Payment Released",
				fmt.Sprintf("₦%.2f has been released to your wallet.", netPayout),
				map[string]interface{}{"booking_id": bk.ID, "amount": netPayout},
			)
			handlers.SendPushToUser(bk.ArtisanID, "Payment Released",
				fmt.Sprintf("₦%.2f released to your wallet (after 8%% platform fee).", netPayout))

			if clientEmail != "" {
				utils.SendBookingCompletedEmail(clientEmail, clientFirstName, artisanFirstName, serviceName, bk.BookingDate)
			}
			if clientPhone != "" {
				utils.SendBookingCompletedSMS(clientPhone, clientFirstName, artisanFirstName, serviceName)
			}

		} else {
			utils.CreateNotification(bgCtx, bk.ClientID,
				utils.NotifBookingCompleted,
				"Artisan Has Marked Your Booking Complete",
				"Please confirm that the service was delivered to release the artisan's payment.",
				map[string]interface{}{"booking_id": bk.ID},
			)
			handlers.SendPushToUser(bk.ClientID,
				"Confirm Booking Completion",
				fmt.Sprintf("%s has marked your booking as complete. Tap to confirm.", artisanFirstName),
			)
		}
	}(fullyComplete, role, bk, artisanCompletedAt)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": responseMessage,
		"booking": bk,
	})
}

// ============================================================================
// GET /bookings  +  GET /bookings/{id}
// (unchanged from original — reproduced here for completeness)
// ============================================================================

// GetMyBookings godoc
// @Summary      List bookings
// @Description  Returns a paginated list of bookings. Clients see bookings they created; artisans see bookings assigned to them. Optionally filter by status.
// @Tags         Booking
// @Produce      json
// @Param        status  query  string  false  "Filter: pending|confirmed|declined|cancelled|completed|disputed"
// @Param        page    query  int     false  "Page (default 1)"
// @Param        limit   query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]booking.Booking,pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Router       /bookings [get]
// @Security     BearerAuth
func GetMyBookings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	statusFilter := r.URL.Query().Get("status")
	validStatuses := map[string]bool{
		"pending": true, "confirmed": true, "declined": true,
		"cancelled": true, "completed": true, "disputed": true,
	}
	if statusFilter != "" && !validStatuses[statusFilter] {
		utils.WriteError(w, "invalid status filter", http.StatusBadRequest)
		return
	}

	whereCol := "client_id"
	if role == "artisan" {
		whereCol = "artisan_id"
	}

	args := []interface{}{userID}
	where := whereCol + " = $1"
	argIdx := 2
	if statusFilter != "" {
		where += " AND status = $2"
		args = append(args, statusFilter)
		argIdx = 3
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var total int
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM artisan_bookings WHERE "+where, args...).Scan(&total)

	fetchArgs := append(args, limit, offset)
	rows, err := db.Query(ctx, `
		SELECT id, client_id, artisan_id, category_id,
		       service_id, service_option_id,
		       booking_date::TEXT, start_time::TEXT, end_time::TEXT,
		       total_price, address, note,
		       status, payment_method, payment_status,
		       confirmed_at, declined_at, cancelled_at, cancelled_by,
		       completed_at, created_at, updated_at
		FROM artisan_bookings
		WHERE `+where+`
		ORDER BY created_at DESC
		LIMIT $`+handlers.Itoa(argIdx)+` OFFSET $`+handlers.Itoa(argIdx+1),
		fetchArgs...,
	)
	if err != nil {
		utils.Logger.Errorf("failed to list bookings: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	bookings := make([]booking.Booking, 0)
	for rows.Next() {
		var bk booking.Booking
		if err := rows.Scan(
			&bk.ID, &bk.ClientID, &bk.ArtisanID, &bk.CategoryID,
			&bk.ServiceID, &bk.ServiceOptionID,
			&bk.BookingDate, &bk.StartTime, &bk.EndTime,
			&bk.TotalPrice, &bk.Address, &bk.Note,
			&bk.Status, &bk.PaymentMethod, &bk.PaymentStatus,
			&bk.ConfirmedAt, &bk.DeclinedAt, &bk.CancelledAt, &bk.CancelledBy,
			&bk.CompletedAt, &bk.CreatedAt, &bk.UpdatedAt,
		); err != nil {
			utils.Logger.Errorf("failed to scan booking: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		bookings = append(bookings, bk)
	}

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(bookings),
		"data":   bookings,
		"pagination": map[string]int{
			"total": total, "page": page,
			"limit": limit, "total_pages": totalPages,
		},
	})
}

// GetBooking godoc
// @Summary      Get a single booking
// @Description  Returns full detail for a booking. The caller must be the client or the artisan on the booking.
// @Tags         Booking
// @Produce      json
// @Param        id  path  string  true  "Booking UUID"
// @Success      200  {object}  object{status=string,data=booking.Booking}
// @Failure      404  {object}  object{error=string}
// @Router       /bookings/{id} [get]
// @Security     BearerAuth
func GetBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var bk booking.Booking
	err = db.QueryRow(ctx, `
		SELECT id, client_id, artisan_id, category_id,
		       service_id, service_option_id,
		       booking_date::TEXT, start_time::TEXT, end_time::TEXT,
		       total_price, address, note,
		       status, payment_method, payment_status,
		       confirmed_at, declined_at, cancelled_at, cancelled_by,
		       completed_at, created_at, updated_at
		FROM artisan_bookings
		WHERE id = $1 AND (client_id = $2 OR artisan_id = $2)
	`, bookingID, userID).Scan(
		&bk.ID, &bk.ClientID, &bk.ArtisanID, &bk.CategoryID,
		&bk.ServiceID, &bk.ServiceOptionID,
		&bk.BookingDate, &bk.StartTime, &bk.EndTime,
		&bk.TotalPrice, &bk.Address, &bk.Note,
		&bk.Status, &bk.PaymentMethod, &bk.PaymentStatus,
		&bk.ConfirmedAt, &bk.DeclinedAt, &bk.CancelledAt, &bk.CancelledBy,
		&bk.CompletedAt, &bk.CreatedAt, &bk.UpdatedAt,
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

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data":   bk,
	})
}
