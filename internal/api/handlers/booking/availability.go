package booking

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"leti_server/internal/models/booking"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
)

type AvailabilityResponse struct {
	Status       string               `json:"status"`
	Message      string               `json:"message"`
	Availability booking.Availability `json:"availability"`
}

type AvailabilityListResponse struct {
	Status string                 `json:"status"`
	Count  int                    `json:"count"`
	Data   []booking.Availability `json:"data"`
}

type AvailabilityOverrideResponse struct {
	Status   string                       `json:"status"`
	Message  string                       `json:"message"`
	Override booking.AvailabilityOverride `json:"override"`
}

type AvailableSlotsResponse struct {
	Status string                  `json:"status"`
	Count  int                     `json:"count"`
	Data   []booking.AvailableSlot `json:"data"`
}

type ArtisanAvailabilityResponse struct {
	Status    string                         `json:"status"`
	Weekly    []booking.Availability         `json:"weekly"`
	Overrides []booking.AvailabilityOverride `json:"overrides"`
}

// ============================================================================
// POST /bookings/artisan/availability
// ============================================================================

// SetAvailability godoc
// @Summary      Set weekly availability window
// @Description  Adds or updates a recurring weekly time window for the authenticated artisan within a service category. Multiple windows per day are allowed (e.g. morning + afternoon). If a window with the same artisan/category/weekday/start_time already exists it is updated; otherwise a new row is inserted.
// @Tags         Artisan / Availability
// @Accept       json
// @Produce      json
// @Param        body  body  object{category_id=string,weekday=int,start_time=string,end_time=string}  true  "weekday: 0=Sun…6=Sat; times in HH:MM format; category_id is a UUID"
// @Success 200 {object} AvailabilityResponse
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Router       /bookings/artisan/availability [post]
// @Security     BearerAuth
func SetAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
		utils.WriteError(w, "only artisans can set availability", http.StatusForbidden)
		return
	}

	type request struct {
		CategoryID uuid.UUID `json:"category_id"`
		Weekday    int       `json:"weekday"`
		StartTime  string    `json:"start_time"`
		EndTime    string    `json:"end_time"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.CategoryID == uuid.Nil {
		utils.WriteError(w, "category_id is required", http.StatusBadRequest)
		return
	}
	if req.Weekday < 0 || req.Weekday > 6 {
		utils.WriteError(w, "weekday must be 0 (Sunday) through 6 (Saturday)", http.StatusBadRequest)
		return
	}
	if req.StartTime == "" || req.EndTime == "" {
		utils.WriteError(w, "start_time and end_time are required (HH:MM)", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("15:04", req.StartTime); err != nil {
		utils.WriteError(w, "start_time must be in HH:MM format", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("15:04", req.EndTime); err != nil {
		utils.WriteError(w, "end_time must be in HH:MM format", http.StatusBadRequest)
		return
	}
	if req.StartTime >= req.EndTime {
		utils.WriteError(w, "end_time must be after start_time", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM artisan_categories WHERE artisan_id=$1 AND category_id=$2)`,
		userID, req.CategoryID,
	).Scan(&exists)
	if err != nil || !exists {
		utils.WriteError(w, "you do not have this category registered", http.StatusBadRequest)
		return
	}

	var av booking.Availability
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_availability
		    (artisan_id, category_id, weekday, start_time, end_time, is_active)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		ON CONFLICT (artisan_id, category_id, weekday, start_time)
		DO UPDATE SET
		    end_time   = EXCLUDED.end_time,
		    is_active  = TRUE,
		    updated_at = NOW()
		RETURNING id, artisan_id, category_id, weekday,
		          start_time::TEXT, end_time::TEXT,
		          is_active, created_at, updated_at
	`, userID, req.CategoryID, req.Weekday, req.StartTime, req.EndTime,
	).Scan(
		&av.ID, &av.ArtisanID, &av.CategoryID, &av.Weekday,
		&av.StartTime, &av.EndTime, &av.IsActive,
		&av.CreatedAt, &av.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to upsert availability: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":       "success",
		"message":      "availability window saved",
		"availability": av,
	})
}

// ============================================================================
// DELETE /bookings/artisan/availability/{id}
// ============================================================================

// DeleteAvailability godoc
// @Summary      Remove a weekly availability window
// @Description  Deactivates a recurring availability window by ID. Existing confirmed bookings are NOT affected.
// @Tags         Artisan / Availability
// @Produce      json
// @Param        id   path  string  true  "Availability UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /bookings/artisan/availability/{id} [delete]
// @Security     BearerAuth
func DeleteAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
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

	avID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid availability id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result, err := db.Exec(ctx, `
		UPDATE artisan_availability SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1 AND artisan_id = $2
	`, avID, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "availability window not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "availability window removed",
	})
}

// ============================================================================
// GET /bookings/artisan/availability
// ============================================================================

// GetMyAvailability godoc
// @Summary      Get own weekly availability
// @Description  Returns all active recurring availability windows for the authenticated artisan, optionally filtered by category.
// @Tags         Artisan / Availability
// @Produce      json
// @Param        category_id  query  string  false  "Filter by category UUID"
// @Success 200  {object}  AvailabilityListResponse
// @Router       /bookings/artisan/availability [get]
// @Security     BearerAuth
func GetMyAvailability(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	query := `
		SELECT id, artisan_id, category_id, weekday,
		       start_time::TEXT, end_time::TEXT,
		       is_active, created_at, updated_at
		FROM artisan_availability
		WHERE artisan_id = $1 AND is_active = TRUE`
	args := []interface{}{userID}

	if catStr := r.URL.Query().Get("category_id"); catStr != "" {
		catID, err := uuid.Parse(catStr)
		if err != nil {
			utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
			return
		}
		query += " AND category_id = $2"
		args = append(args, catID)
	}
	query += " ORDER BY weekday, start_time"

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		utils.Logger.Errorf("failed to fetch availability: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]booking.Availability, 0)
	for rows.Next() {
		var av booking.Availability
		if err := rows.Scan(
			&av.ID, &av.ArtisanID, &av.CategoryID, &av.Weekday,
			&av.StartTime, &av.EndTime, &av.IsActive,
			&av.CreatedAt, &av.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		items = append(items, av)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(items),
		"data":   items,
	})
}

// ============================================================================
// POST /bookings/artisan/availability/overrides
// ============================================================================

// SetAvailabilityOverride godoc
// @Summary      Block or open a specific date
// @Description  Creates or replaces a date-level override for the artisan. Set is_available=false to block a day (e.g. public holiday, personal day). Set is_available=true to force-open a day that would normally be off.
// @Tags         Artisan / Availability
// @Accept       json
// @Produce      json
// @Param        body  body  object{category_id=string,date=string,is_available=bool,note=string}  true  "date in YYYY-MM-DD format; category_id is a UUID"
// @Success      200   {object}  AvailabilityOverrideResponse
// @Failure      400   {object}  object{error=string}
// @Router       /bookings/artisan/availability/overrides [post]
// @Security     BearerAuth
func SetAvailabilityOverride(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
		utils.WriteError(w, "only artisans can set availability overrides", http.StatusForbidden)
		return
	}

	type request struct {
		CategoryID  uuid.UUID `json:"category_id"`
		Date        string    `json:"date"`
		IsAvailable bool      `json:"is_available"`
		Note        *string   `json:"note,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.CategoryID == uuid.Nil {
		utils.WriteError(w, "category_id is required", http.StatusBadRequest)
		return
	}
	if req.Date == "" {
		utils.WriteError(w, "date is required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	parsedDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		utils.WriteError(w, "date must be in YYYY-MM-DD format", http.StatusBadRequest)
		return
	}
	if parsedDate.Before(time.Now().Truncate(24 * time.Hour)) {
		utils.WriteError(w, "cannot set override for a past date", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var ov booking.AvailabilityOverride
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_availability_overrides
		    (artisan_id, category_id, override_date, is_available, note)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (artisan_id, category_id, override_date)
		DO UPDATE SET
		    is_available = EXCLUDED.is_available,
		    note         = EXCLUDED.note
		RETURNING id, artisan_id, category_id,
		          override_date::TEXT, is_available, note, created_at
	`, userID, req.CategoryID, req.Date, req.IsAvailable, req.Note,
	).Scan(
		&ov.ID, &ov.ArtisanID, &ov.CategoryID,
		&ov.OverrideDate, &ov.IsAvailable, &ov.Note, &ov.CreatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to upsert override: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	msg := "date marked as unavailable"
	if req.IsAvailable {
		msg = "date marked as available"
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  msg,
		"override": ov,
	})
}

// ============================================================================
// GET /bookings/artisans/{id}/available-slots
// ============================================================================

// GetAvailableSlots godoc
// @Summary      Get available slots for an artisan
// @Description  Returns a day-by-day availability breakdown for an artisan within a given date range (max 60 days). For each date: checks the weekday recurring schedule, applies any date-level overrides, then subtracts confirmed/pending bookings to determine if the slot is still open.
// @Tags         Booking
// @Produce      json
// @Param        id           path   string  true   "Artisan UUID"
// @Param        category_id  query  string  true   "Category UUID"
// @Param        from         query  string  true   "Start date YYYY-MM-DD (inclusive)"
// @Param        to           query  string  true   "End date YYYY-MM-DD (inclusive, max 60 days from from)"
// @Success      200  {object}  AvailableSlotsResponse
// @Failure      400  {object}  object{error=string}
// @Router       /bookings/artisans/{id}/available-slots [get]
// @Security     BearerAuth
func GetAvailableSlots(w http.ResponseWriter, r *http.Request) {
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

	catStr := r.URL.Query().Get("category_id")
	if catStr == "" {
		utils.WriteError(w, "category_id is required", http.StatusBadRequest)
		return
	}
	categoryID, err := uuid.Parse(catStr)
	if err != nil {
		utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		utils.WriteError(w, "from and to dates are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		utils.WriteError(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		utils.WriteError(w, "to must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if to.Before(from) {
		utils.WriteError(w, "to must be on or after from", http.StatusBadRequest)
		return
	}
	if to.Sub(from) > 60*24*time.Hour {
		utils.WriteError(w, "date range cannot exceed 60 days", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// 1. Fetch all active weekly windows for this artisan + category
	type window struct {
		weekday   int
		startTime string
		endTime   string
	}
	windowRows, err := db.Query(ctx, `
		SELECT weekday, start_time::TEXT, end_time::TEXT
		FROM artisan_availability
		WHERE artisan_id = $1 AND category_id = $2 AND is_active = TRUE
		ORDER BY weekday, start_time
	`, artisanID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch availability windows: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer windowRows.Close()

	weekdayWindows := make(map[int][]window)
	for windowRows.Next() {
		var ww window
		if err := windowRows.Scan(&ww.weekday, &ww.startTime, &ww.endTime); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		weekdayWindows[ww.weekday] = append(weekdayWindows[ww.weekday], ww)
	}

	// 2. Fetch date-level overrides in the range
	type override struct {
		isAvailable bool
	}
	overrideMap := make(map[string]override)
	ovRows, err := db.Query(ctx, `
		SELECT override_date::TEXT, is_available
		FROM artisan_availability_overrides
		WHERE artisan_id = $1 AND category_id = $2
		  AND override_date BETWEEN $3 AND $4
	`, artisanID, categoryID, fromStr, toStr)
	if err != nil {
		utils.Logger.Errorf("failed to fetch overrides: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer ovRows.Close()

	for ovRows.Next() {
		var dateStr string
		var isAv bool
		if err := ovRows.Scan(&dateStr, &isAv); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		overrideMap[dateStr] = override{isAvailable: isAv}
	}

	// 3. Fetch confirmed/pending bookings in the range (block the slot)
	bookedSet := make(map[string]bool) // key = "YYYY-MM-DD|HH:MM"
	bkRows, err := db.Query(ctx, `
		SELECT booking_date::TEXT, start_time::TEXT
		FROM artisan_bookings
		WHERE artisan_id = $1 AND category_id = $2
		  AND booking_date BETWEEN $3 AND $4
		  AND status IN ('pending', 'confirmed')
	`, artisanID, categoryID, fromStr, toStr)
	if err != nil {
		utils.Logger.Errorf("failed to fetch bookings: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer bkRows.Close()

	for bkRows.Next() {
		var dateStr, startStr string
		if err := bkRows.Scan(&dateStr, &startStr); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		bookedSet[dateStr+"|"+startStr] = true
	}

	// 4. Walk every day in range, produce AvailableSlot entries
	slots := make([]booking.AvailableSlot, 0)
	today := time.Now().Truncate(24 * time.Hour)

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		weekday := int(d.Weekday()) // 0=Sun

		// Past dates are never available
		if d.Before(today) {
			continue
		}

		// Check override first
		if ov, hasOv := overrideMap[dateStr]; hasOv {
			if !ov.isAvailable {
				// Entire day blocked by override — emit a single unavailable slot
				slots = append(slots, booking.AvailableSlot{
					Date:      dateStr,
					StartTime: "00:00",
					EndTime:   "23:59",
					Available: false,
				})
				continue
			}
			// is_available=true: override forces open — fall through to emit windows
		}

		windows := weekdayWindows[weekday]
		for _, ww := range windows {
			key := dateStr + "|" + ww.startTime
			available := !bookedSet[key]
			slots = append(slots, booking.AvailableSlot{
				Date:      dateStr,
				StartTime: ww.startTime,
				EndTime:   ww.endTime,
				Available: available,
			})
		}
		// If no windows defined for this weekday and no override, the day simply
		// doesn't appear in the response (artisan doesn't work that day).
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(slots),
		"data":   slots,
	})
}

// ============================================================================
// GET /bookings/artisans/{id}/availability
// ============================================================================

// GetArtisanAvailability godoc
// @Summary      Get an artisan's public availability schedule
// @Description  Returns the recurring weekly schedule and any upcoming date overrides for a specific artisan/category. Useful for displaying a calendar UI before choosing a date.
// @Tags         Booking
// @Produce      json
// @Param        id           path   string  true   "Artisan UUID"
// @Param        category_id  query  string  true   "Category UUID"
// @Success      200  {object}  ArtisanAvailabilityResponse
// @Failure      400  {object}  object{error=string}
// @Router       /bookings/artisan/{id}/availability [get]
func GetArtisanAvailability(w http.ResponseWriter, r *http.Request) {
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

	catStr := r.URL.Query().Get("category_id")
	if catStr == "" {
		utils.WriteError(w, "category_id is required", http.StatusBadRequest)
		return
	}
	categoryID, err := uuid.Parse(catStr)
	if err != nil {
		utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Weekly windows
	wRows, err := db.Query(ctx, `
		SELECT id, artisan_id, category_id, weekday,
		       start_time::TEXT, end_time::TEXT,
		       is_active, created_at, updated_at
		FROM artisan_availability
		WHERE artisan_id = $1 AND category_id = $2 AND is_active = TRUE
		ORDER BY weekday, start_time
	`, artisanID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch weekly windows: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer wRows.Close()

	weekly := make([]booking.Availability, 0)
	for wRows.Next() {
		var av booking.Availability
		if err := wRows.Scan(
			&av.ID, &av.ArtisanID, &av.CategoryID, &av.Weekday,
			&av.StartTime, &av.EndTime, &av.IsActive,
			&av.CreatedAt, &av.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		weekly = append(weekly, av)
	}

	// Upcoming overrides (next 90 days)
	until := time.Now().AddDate(0, 3, 0).Format("2006-01-02")
	ovRows, err := db.Query(ctx, `
		SELECT id, artisan_id, category_id,
		       override_date::TEXT, is_available, note, created_at
		FROM artisan_availability_overrides
		WHERE artisan_id = $1 AND category_id = $2
		  AND override_date >= CURRENT_DATE
		  AND override_date <= $3
		ORDER BY override_date
	`, artisanID, categoryID, until)
	if err != nil {
		utils.Logger.Errorf("failed to fetch overrides: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer ovRows.Close()

	overrides := make([]booking.AvailabilityOverride, 0)
	for ovRows.Next() {
		var ov booking.AvailabilityOverride
		if err := ovRows.Scan(
			&ov.ID, &ov.ArtisanID, &ov.CategoryID,
			&ov.OverrideDate, &ov.IsAvailable, &ov.Note, &ov.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		overrides = append(overrides, ov)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":    "success",
		"weekly":    weekly,
		"overrides": overrides,
	})
}
