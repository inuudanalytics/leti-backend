package booking

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/models/booking"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ArtisanServiceSummary struct {
	ServiceID   uuid.UUID `json:"service_id"`
	ServiceName string    `json:"service_name"`
	BasePrice   float64   `json:"base_price"`
	CategoryID  uuid.UUID `json:"category_id"`
}

type ArtisanListItem struct {
	UserID      uuid.UUID               `json:"user_id"`
	Username    string                  `json:"username"`
	Avatar      interface{}             `json:"avatar"`
	IsOnline    bool                    `json:"is_online"`
	AvgRating   float64                 `json:"avg_rating"`
	ReviewCount int                     `json:"review_count"`
	Categories  []CategorySummary       `json:"categories"`
	Services    []ArtisanServiceSummary `json:"services"`
	Location    *LocationPoint          `json:"location,omitempty"`
	DistanceKm  *float64                `json:"distance_km,omitempty"`
}

type CategorySummary struct {
	CategoryID   uuid.UUID `json:"category_id"`
	CategoryName string    `json:"category_name"`
}

type LocationPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ReviewItem represents a single review with reviewer info.
type ReviewItem struct {
	ReviewID  uuid.UUID   `json:"review_id"`
	ClientID  uuid.UUID   `json:"client_id"`
	Username  string      `json:"username"`
	Avatar    interface{} `json:"avatar"`
	Rating    int         `json:"rating"`
	Comment   *string     `json:"comment,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// ServiceVariationOption is a single option within a variation type.
type ServiceVariationOption struct {
	OptionID      uuid.UUID `json:"option_id"`
	Label         string    `json:"label"`
	PriceModifier float64   `json:"price_modifier"`
}

// ServiceVariationType groups options for one variation axis.
type ServiceVariationType struct {
	VariationTypeID    uuid.UUID                `json:"variation_type_id"`
	VariationTypeLabel string                   `json:"variation_type_label"`
	Options            []ServiceVariationOption `json:"options"`
}

// ServiceDetail is a full service with its variation tree.
type ServiceDetail struct {
	ServiceID   uuid.UUID              `json:"service_id"`
	ServiceName string                 `json:"service_name"`
	Description *string                `json:"description,omitempty"`
	BasePrice   float64                `json:"base_price"`
	CategoryID  uuid.UUID              `json:"category_id"`
	Variations  []ServiceVariationType `json:"variations"`
}

// PortfolioImage is one portfolio entry.
type PortfolioImage struct {
	ImageID    uuid.UUID `json:"image_id"`
	ImageURL   string    `json:"image_url"`
	Caption    *string   `json:"caption,omitempty"`
	CategoryID uuid.UUID `json:"category_id"`
	SortOrder  int       `json:"sort_order"`
}

// ArtisanProfileDetail is the full single-artisan response.
type ArtisanProfileDetail struct {
	UserID        uuid.UUID         `json:"user_id"`
	Username      string            `json:"username"`
	Avatar        interface{}       `json:"avatar"`
	Bio           *string           `json:"bio,omitempty"`
	IsOnline      bool              `json:"is_online"`
	IsVerified    bool              `json:"is_verified"`
	AvgRating     float64           `json:"avg_rating"`
	ReviewCount   int               `json:"review_count"`
	MemberSince   time.Time         `json:"member_since"`
	MemberForDays int               `json:"member_for_days"`
	Categories    []CategorySummary `json:"categories"`
	Portfolio     []PortfolioImage  `json:"portfolio"`
	Services      []ServiceDetail   `json:"services"`
	Reviews       []ReviewItem      `json:"reviews"`
}

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

		var artisanEmail, artisanPhone, artisanUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
			artisanID,
		).Scan(&artisanEmail, &artisanPhone, &artisanUsername)

		var clientUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT username FROM users WHERE id = $1`, clientID,
		).Scan(&clientUsername)

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
				artisanUsername,
				clientUsername,
				serviceName,
				bk.BookingDate,
				bk.StartTime,
			)
		}

		// SMS
		if artisanPhone != "" {
			utils.SendBookingRequestSMS(
				artisanPhone,
				artisanUsername,
				clientUsername,
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

		var clientEmail, clientPhone, clientUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
			bk.ClientID,
		).Scan(&clientEmail, &clientPhone, &clientUsername)

		var artisanUsername, serviceName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT username FROM users WHERE id = $1`, bk.ArtisanID,
		).Scan(&artisanUsername)

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
				clientUsername,
				artisanUsername,
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
				clientUsername,
				artisanUsername,
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

		var clientEmail, clientPhone, clientUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
			clientID,
		).Scan(&clientEmail, &clientPhone, &clientUsername)

		var artisanUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT username FROM users WHERE id = $1`, artisanID,
		).Scan(&artisanUsername)

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
				clientUsername,
				artisanUsername,
				serviceName,
				bookingDate,
			)
		}

		// SMS
		if clientPhone != "" {
			utils.SendBookingDeclinedSMS(
				clientPhone,
				clientUsername,
				artisanUsername,
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
		var notifyEmail, notifyPhone, notifyUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
			notifyID,
		).Scan(&notifyEmail, &notifyPhone, &notifyUsername)

		var cancellerUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT username FROM users WHERE id = $1`, userID,
		).Scan(&cancellerUsername)

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
				notifyUsername,
				cancellerUsername,
				serviceName,
				bookingDate,
				cancelledByRole,
			)
		}

		// SMS
		if notifyPhone != "" {
			utils.SendBookingCancelledSMS(
				notifyPhone,
				notifyUsername,
				cancellerUsername,
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

		var artisanUsername, serviceName string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT username FROM users WHERE id = $1`, bk.ArtisanID,
		).Scan(&artisanUsername)

		var clientEmail, clientPhone, clientUsername string
		_ = sqlconnect.DB.QueryRow(bgCtx,
			`SELECT COALESCE(email,''), COALESCE(phone_number,''), username FROM users WHERE id = $1`,
			bk.ClientID,
		).Scan(&clientEmail, &clientPhone, &clientUsername)

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
				fmt.Sprintf("₦%.2f released to your wallet (after 8%% platform fee).", netPayout),
				map[string]string{
					"screen":     "ArtisanDashboard",
					"booking_id": bk.ID.String(),
				})

			if clientEmail != "" {
				utils.SendBookingCompletedEmail(clientEmail, clientUsername, artisanUsername, serviceName, bk.BookingDate)
			}
			if clientPhone != "" {
				utils.SendBookingCompletedSMS(clientPhone, clientUsername, artisanUsername, serviceName)
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
				fmt.Sprintf("%s has marked your booking as complete. Tap to confirm.", artisanUsername),
				map[string]string{
					"screen":     "BookingDetails",
					"booking_id": bk.ID.String(),
				})
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

// ============================================================================
// GET /artisans/online
// ============================================================================

// GetOnlineArtisans godoc
// @Summary      List online artisans
// @Description  Returns a paginated list of artisans whose online status is TRUE.
//
// Supports the following optional query filters:
//
//   - category_id (uuid)      – filter by a specific job category
//   - service_name (string)   – partial match on artisan service name (case-insensitive)
//   - username (string)       – partial match on artisan username (case-insensitive)
//   - top_rated (bool)        – if true, sort by avg_rating DESC; minimum 4.0 rating
//   - lat (float64)           – client latitude for nearby filter
//   - lng (float64)           – client longitude for nearby filter
//   - radius_km (float64)     – radius in km around lat/lng (default 10)
//   - page (int)              – page number, default 1
//   - limit (int)             – results per page, default 20, max 50
//
// @Tags         Artisan Discovery
// @Produce      json
// @Param        category_id   query  string  false  "Filter by category UUID"
// @Param        service_name  query  string  false  "Partial match on service name"
// @Param        username      query  string  false  "Partial match on artisan username"
// @Param        top_rated     query  bool    false  "Sort by top rated (min rating 4.0)"
// @Param        lat           query  number  false  "Client latitude for nearby search"
// @Param        lng           query  number  false  "Client longitude for nearby search"
// @Param        radius_km     query  number  false  "Radius in km (default 10)"
// @Param        page          query  int     false  "Page number (default 1)"
// @Param        limit         query  int     false  "Results per page (default 20, max 50)"
// @Success      200  {object}  object{data=[]ArtisanListItem,total=int,page=int,limit=int}
// @Failure      400  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /artisans/online [get]
// @Security     BearerAuth
func GetOnlineArtisans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()

	page := 1
	limit := 20
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 50 {
				n = 50
			}
			limit = n
		}
	}
	offset := (page - 1) * limit

	categoryIDStr := strings.TrimSpace(q.Get("category_id"))
	serviceName := strings.TrimSpace(q.Get("service_name"))
	username := strings.TrimSpace(q.Get("username"))
	topRated := q.Get("top_rated") == "true" || q.Get("top_rated") == "1"

	var nearbyLat, nearbyLng *float64
	var radiusKm float64 = 10
	if latStr := q.Get("lat"); latStr != "" {
		if v, err := strconv.ParseFloat(latStr, 64); err == nil {
			nearbyLat = &v
		}
	}
	if lngStr := q.Get("lng"); lngStr != "" {
		if v, err := strconv.ParseFloat(lngStr, 64); err == nil {
			nearbyLng = &v
		}
	}
	if rStr := q.Get("radius_km"); rStr != "" {
		if v, err := strconv.ParseFloat(rStr, 64); err == nil && v > 0 {
			radiusKm = v
		}
	}

	args := []interface{}{}
	argIdx := 1

	conditions := []string{"u.is_online = TRUE", "u.deleted_at IS NULL", "u.status = 'approved'"}

	if categoryIDStr != "" {
		if _, err := uuid.Parse(categoryIDStr); err != nil {
			utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
			return
		}
		conditions = append(conditions,
			fmt.Sprintf(`EXISTS (
				SELECT 1 FROM artisan_categories ac
				WHERE ac.artisan_id = u.id AND ac.category_id = $%d
			)`, argIdx))
		args = append(args, categoryIDStr)
		argIdx++
	}

	if serviceName != "" {
		conditions = append(conditions,
			fmt.Sprintf(`EXISTS (
				SELECT 1 FROM artisan_services asv
				WHERE asv.artisan_id = u.id AND asv.is_active = TRUE
				  AND LOWER(asv.name) LIKE LOWER($%d)
			)`, argIdx))
		args = append(args, "%"+serviceName+"%")
		argIdx++
	}

	if username != "" {
		conditions = append(conditions,
			fmt.Sprintf(`LOWER(u.username) LIKE LOWER($%d)`, argIdx))
		args = append(args, "%"+username+"%")
		argIdx++
	}

	if topRated {
		conditions = append(conditions, `(
			SELECT COALESCE(ROUND(AVG(ar.rating)::NUMERIC, 2), 0)
			FROM artisan_reviews ar WHERE ar.artisan_id = u.id
		) >= 4.0`)
	}

	var distanceExpr string
	if nearbyLat != nil && nearbyLng != nil {
		conditions = append(conditions,
			fmt.Sprintf(`EXISTS (
				SELECT 1 FROM artisan_address aa
				WHERE aa.artisan_id = u.id
				  AND ST_DWithin(
				      aa.location::geography,
				      ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography,
				      $%d
				  )
			)`, argIdx, argIdx+1, argIdx+2))
		args = append(args, *nearbyLng, *nearbyLat, radiusKm*1000)
		argIdx += 3

		distanceExpr = fmt.Sprintf(`(
			SELECT ROUND(
				ST_Distance(
					aa2.location::geography,
					ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography
				) / 1000, 2
			)
			FROM artisan_address aa2
			WHERE aa2.artisan_id = u.id
			ORDER BY aa2.is_primary DESC
			LIMIT 1
		) AS distance_km`, argIdx, argIdx+1)
		args = append(args, *nearbyLng, *nearbyLat)
		argIdx += 2
	} else {
		distanceExpr = "NULL AS distance_km"
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	orderBy := "ORDER BY u.is_online DESC"
	if topRated {
		orderBy = `ORDER BY (
			SELECT COALESCE(ROUND(AVG(ar.rating)::NUMERIC, 2), 0)
			FROM artisan_reviews ar WHERE ar.artisan_id = u.id
		) DESC NULLS LAST`
	} else if nearbyLat != nil && nearbyLng != nil {
		orderBy = "ORDER BY distance_km ASC NULLS LAST"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	countSQL := fmt.Sprintf(`
		SELECT COUNT(DISTINCT u.id)
		FROM users u
		%s
	`, whereClause)

	var total int
	if err := db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		utils.Logger.Errorf("failed to count artisans: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	paginationArgs := append(args, limit, offset)
	limitIdx := argIdx
	offsetIdx := argIdx + 1

	mainSQL := fmt.Sprintf(`
		SELECT
			u.id,
			u.username,
			u.avatar,
			u.is_online,
			COALESCE(ROUND(AVG(ar.rating)::NUMERIC, 2), 0.00) AS avg_rating,
			COUNT(ar.id)                                       AS review_count,
			%s
		FROM users u
		LEFT JOIN artisan_reviews ar ON ar.artisan_id = u.id
		%s
		GROUP BY u.id
		%s
		LIMIT $%d OFFSET $%d
	`, distanceExpr, whereClause, orderBy, limitIdx, offsetIdx)

	rows, err := db.Query(ctx, mainSQL, paginationArgs...)
	if err != nil {
		utils.Logger.Errorf("failed to query artisans: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type rawRow struct {
		UserID      uuid.UUID
		Username    string
		Avatar      interface{}
		IsOnline    bool
		AvgRating   float64
		ReviewCount int
		DistanceKm  *float64
	}

	var rawRows []rawRow
	var artisanIDs []uuid.UUID

	for rows.Next() {
		var rr rawRow
		if err := rows.Scan(
			&rr.UserID, &rr.Username, &rr.Avatar,
			&rr.IsOnline, &rr.AvgRating, &rr.ReviewCount,
			&rr.DistanceKm,
		); err != nil {
			utils.Logger.Errorf("scan artisan row: %v", err)
			continue
		}
		rawRows = append(rawRows, rr)
		artisanIDs = append(artisanIDs, rr.UserID)
	}
	rows.Close()

	if len(artisanIDs) == 0 {
		utils.WriteJSON(w, map[string]interface{}{
			"data":  []interface{}{},
			"total": total,
			"page":  page,
			"limit": limit,
		})
		return
	}

	catMap := map[uuid.UUID][]CategorySummary{}
	catRows, err := db.Query(ctx, `
		SELECT ac.artisan_id, jc.id, jc.name
		FROM artisan_categories ac
		JOIN job_categories jc ON jc.id = ac.category_id
		WHERE ac.artisan_id = ANY($1)
	`, artisanIDs)
	if err == nil {
		defer catRows.Close()
		for catRows.Next() {
			var aid uuid.UUID
			var cs CategorySummary
			if e := catRows.Scan(&aid, &cs.CategoryID, &cs.CategoryName); e == nil {
				catMap[aid] = append(catMap[aid], cs)
			}
		}
	}

	svcMap := map[uuid.UUID][]ArtisanServiceSummary{}
	svcRows, err := db.Query(ctx, `
		SELECT artisan_id, id, name, base_price, category_id
		FROM artisan_services
		WHERE artisan_id = ANY($1) AND is_active = TRUE
	`, artisanIDs)
	if err == nil {
		defer svcRows.Close()
		for svcRows.Next() {
			var aid uuid.UUID
			var s ArtisanServiceSummary
			if e := svcRows.Scan(&aid, &s.ServiceID, &s.ServiceName, &s.BasePrice, &s.CategoryID); e == nil {
				svcMap[aid] = append(svcMap[aid], s)
			}
		}
	}

	locMap := map[uuid.UUID]*LocationPoint{}
	locRows, err := db.Query(ctx, `
		SELECT DISTINCT ON (artisan_id)
			artisan_id,
			ST_Y(location::geometry) AS lat,
			ST_X(location::geometry) AS lng
		FROM artisan_address
		WHERE artisan_id = ANY($1) AND location IS NOT NULL
		ORDER BY artisan_id, is_primary DESC
	`, artisanIDs)
	if err == nil {
		defer locRows.Close()
		for locRows.Next() {
			var aid uuid.UUID
			var lp LocationPoint
			if e := locRows.Scan(&aid, &lp.Latitude, &lp.Longitude); e == nil {
				locMap[aid] = &lp
			}
		}
	}

	result := make([]ArtisanListItem, 0, len(rawRows))
	for _, rr := range rawRows {
		item := ArtisanListItem{
			UserID:      rr.UserID,
			Username:    rr.Username,
			Avatar:      rr.Avatar,
			IsOnline:    rr.IsOnline,
			AvgRating:   rr.AvgRating,
			ReviewCount: rr.ReviewCount,
			Categories:  catMap[rr.UserID],
			Services:    svcMap[rr.UserID],
			Location:    locMap[rr.UserID],
			DistanceKm:  rr.DistanceKm,
		}
		if item.Categories == nil {
			item.Categories = []CategorySummary{}
		}
		if item.Services == nil {
			item.Services = []ArtisanServiceSummary{}
		}
		result = append(result, item)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"data":  result,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// ============================================================================
// GET /artisans/{id}
// ============================================================================

// GetArtisanProfile godoc
// @Summary      Get artisan profile
// @Description  Returns the full public profile of a single artisan by their user UUID.
//
// The response includes:
//   - avatar, username, bio, online status, verification status
//   - total avg rating and number of reviews
//   - how long they have been on the platform (member_since + member_for_days)
//   - portfolio images grouped by category
//   - all services with their variation types and option price modifiers
//   - all reviews (with reviewer username + avatar)
//
// Note: The artisan does NOT have to be online to be viewed via this endpoint.
// Use GET /artisans/online to list only online artisans.
//
// @Tags         Artisan Discovery
// @Produce      json
// @Param        id  path  string  true  "Artisan user UUID"
// @Success      200  {object}  ArtisanProfileDetail
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /artisans/{id} [get]
// @Security     BearerAuth
func GetArtisanProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	artisanID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid artisan id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	type coreUser struct {
		UserID        uuid.UUID
		Username      string
		Avatar        interface{}
		Bio           *string
		IsOnline      bool
		Status        string
		CreatedAt     time.Time
		PhoneVerified bool
		EmailVerified bool
	}
	var cu coreUser

	err = db.QueryRow(ctx, `
		SELECT id, username, avatar, bio, COALESCE(is_online, FALSE),
		       status, user_created_at, phone_verified, email_verified
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, artisanID).Scan(
		&cu.UserID, &cu.Username, &cu.Avatar, &cu.Bio, &cu.IsOnline,
		&cu.Status, &cu.CreatedAt, &cu.PhoneVerified, &cu.EmailVerified,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "artisan not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch artisan user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var hasArtisanRole bool
	_ = db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles WHERE user_id = $1 AND role = 'artisan'
		)
	`, artisanID).Scan(&hasArtisanRole)
	if !hasArtisanRole {
		utils.WriteError(w, "artisan not found", http.StatusNotFound)
		return
	}

	isVerified := cu.PhoneVerified || cu.EmailVerified
	memberForDays := int(math.Floor(time.Since(cu.CreatedAt).Hours() / 24))

	var avgRating float64
	var reviewCount int
	_ = db.QueryRow(ctx, `
		SELECT COALESCE(ROUND(AVG(rating)::NUMERIC, 2), 0.00), COUNT(*)
		FROM artisan_reviews
		WHERE artisan_id = $1
	`, artisanID).Scan(&avgRating, &reviewCount)

	categories := []CategorySummary{}
	catRows, err := db.Query(ctx, `
		SELECT jc.id, jc.name
		FROM artisan_categories ac
		JOIN job_categories jc ON jc.id = ac.category_id
		WHERE ac.artisan_id = $1
	`, artisanID)
	if err == nil {
		defer catRows.Close()
		for catRows.Next() {
			var cs CategorySummary
			if e := catRows.Scan(&cs.CategoryID, &cs.CategoryName); e == nil {
				categories = append(categories, cs)
			}
		}
	}

	portfolio := []PortfolioImage{}
	portRows, err := db.Query(ctx, `
		SELECT id, image_url, caption, category_id, sort_order
		FROM artisan_portfolio_images
		WHERE artisan_id = $1
		ORDER BY category_id, sort_order ASC
	`, artisanID)
	if err == nil {
		defer portRows.Close()
		for portRows.Next() {
			var pi PortfolioImage
			if e := portRows.Scan(&pi.ImageID, &pi.ImageURL, &pi.Caption, &pi.CategoryID, &pi.SortOrder); e == nil {
				portfolio = append(portfolio, pi)
			}
		}
	}

	type rawService struct {
		ServiceID   uuid.UUID
		ServiceName string
		Description *string
		BasePrice   float64
		CategoryID  uuid.UUID
	}
	var rawServices []rawService
	var serviceIDs []uuid.UUID

	svcRows, err := db.Query(ctx, `
		SELECT id, name, description, base_price, category_id
		FROM artisan_services
		WHERE artisan_id = $1 AND is_active = TRUE
		ORDER BY created_at ASC
	`, artisanID)
	if err == nil {
		defer svcRows.Close()
		for svcRows.Next() {
			var rs rawService
			if e := svcRows.Scan(&rs.ServiceID, &rs.ServiceName, &rs.Description, &rs.BasePrice, &rs.CategoryID); e == nil {
				rawServices = append(rawServices, rs)
				serviceIDs = append(serviceIDs, rs.ServiceID)
			}
		}
	}

	type optRow struct {
		ServiceID          uuid.UUID
		VariationTypeID    uuid.UUID
		VariationTypeLabel string
		OptionID           uuid.UUID
		OptionLabel        string
		PriceModifier      float64
	}
	optMap := map[uuid.UUID]map[uuid.UUID]*ServiceVariationType{}

	if len(serviceIDs) > 0 {
		optRows, err := db.Query(ctx, `
			SELECT
				aso.service_id,
				svt.id            AS variation_type_id,
				svt.label         AS variation_type_label,
				aso.id            AS option_id,
				aso.label         AS option_label,
				aso.price_modifier
			FROM artisan_service_options aso
			JOIN service_variation_types svt ON svt.id = aso.variation_type_id
			WHERE aso.service_id = ANY($1)
			ORDER BY svt.id, aso.label
		`, serviceIDs)
		if err == nil {
			defer optRows.Close()
			for optRows.Next() {
				var o optRow
				if e := optRows.Scan(
					&o.ServiceID, &o.VariationTypeID, &o.VariationTypeLabel,
					&o.OptionID, &o.OptionLabel, &o.PriceModifier,
				); e != nil {
					continue
				}
				if optMap[o.ServiceID] == nil {
					optMap[o.ServiceID] = map[uuid.UUID]*ServiceVariationType{}
				}
				vt, ok := optMap[o.ServiceID][o.VariationTypeID]
				if !ok {
					vt = &ServiceVariationType{
						VariationTypeID:    o.VariationTypeID,
						VariationTypeLabel: o.VariationTypeLabel,
						Options:            []ServiceVariationOption{},
					}
					optMap[o.ServiceID][o.VariationTypeID] = vt
				}
				vt.Options = append(vt.Options, ServiceVariationOption{
					OptionID:      o.OptionID,
					Label:         o.OptionLabel,
					PriceModifier: o.PriceModifier,
				})
			}
		}
	}

	services := make([]ServiceDetail, 0, len(rawServices))
	for _, rs := range rawServices {
		variations := []ServiceVariationType{}
		if vtMap, ok := optMap[rs.ServiceID]; ok {
			for _, vt := range vtMap {
				variations = append(variations, *vt)
			}
		}
		services = append(services, ServiceDetail{
			ServiceID:   rs.ServiceID,
			ServiceName: rs.ServiceName,
			Description: rs.Description,
			BasePrice:   rs.BasePrice,
			CategoryID:  rs.CategoryID,
			Variations:  variations,
		})
	}

	reviews := []ReviewItem{}
	revRows, err := db.Query(ctx, `
		SELECT ar.id, ar.client_id, u.username, u.avatar,
		       ar.rating, ar.comment, ar.created_at
		FROM artisan_reviews ar
		JOIN users u ON u.id = ar.client_id
		WHERE ar.artisan_id = $1
		ORDER BY ar.created_at DESC
	`, artisanID)
	if err == nil {
		defer revRows.Close()
		for revRows.Next() {
			var ri ReviewItem
			if e := revRows.Scan(
				&ri.ReviewID, &ri.ClientID, &ri.Username, &ri.Avatar,
				&ri.Rating, &ri.Comment, &ri.CreatedAt,
			); e == nil {
				reviews = append(reviews, ri)
			}
		}
	}

	profile := ArtisanProfileDetail{
		UserID:        cu.UserID,
		Username:      cu.Username,
		Avatar:        cu.Avatar,
		Bio:           cu.Bio,
		IsOnline:      cu.IsOnline,
		IsVerified:    isVerified,
		AvgRating:     avgRating,
		ReviewCount:   reviewCount,
		MemberSince:   cu.CreatedAt,
		MemberForDays: memberForDays,
		Categories:    categories,
		Portfolio:     portfolio,
		Services:      services,
		Reviews:       reviews,
	}

	utils.WriteJSON(w, profile)
}
