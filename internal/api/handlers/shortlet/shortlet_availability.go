package shortlet

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	shortletcache "leti_server/internal/api/handlers/shortlet/shortletcache"
	"leti_server/internal/models/shortlet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PropertyAvailabilityResponse struct {
	Status       string                        `json:"status"`
	Message      string                        `json:"message"`
	Availability shortlet.PropertyAvailability `json:"availability"`
}

type PropertyAvailabilityOverrideResponse struct {
	Status   string                                `json:"status"`
	Message  string                                `json:"message"`
	Override shortlet.PropertyAvailabilityOverride `json:"override"`
}

type PropertyCalendarResponse struct {
	Status       string                 `json:"status"`
	Count        int                    `json:"count"`
	CheckInTime  string                 `json:"check_in_time"`
	CheckOutTime string                 `json:"check_out_time"`
	Data         []shortlet.CalendarDay `json:"data"`
}

type SavedListingToggleResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Saved   bool   `json:"saved"`
}

type SavedListingsResponse struct {
	Status     string              `json:"status"`
	Count      int                 `json:"count"`
	Data       []shortlet.Property `json:"data"`
	Pagination map[string]int      `json:"pagination"`
}

// ============================================================================
// POST /properties/{id}/availability
// ============================================================================

// SetPropertyAvailability godoc
// @Summary      Set availability window for a property
// @Description  Creates or updates an availability date range for the property. Multiple non-overlapping windows are allowed (e.g. different months). If a range with the same from/to already exists it is updated in place.
// @Tags         Properties / Availability
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Property UUID"
// @Param        body  body  object{available_from=string,available_to=string,check_in_time=string,check_out_time=string}  true  "Dates in YYYY-MM-DD, times in HH:MM (default 14:00 / 11:00)"
// @Success 200 {object} PropertyAvailabilityResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /properties/{id}/availability [post]
// @Security     BearerAuth
func SetPropertyAvailability(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only owners can manage availability", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	type request struct {
		AvailableFrom string `json:"available_from"`
		AvailableTo   string `json:"available_to"`
		CheckInTime   string `json:"check_in_time"`
		CheckOutTime  string `json:"check_out_time"`
	}
	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.AvailableFrom == "" || req.AvailableTo == "" {
		utils.WriteError(w, "available_from and available_to are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	fromDate, err := time.Parse("2006-01-02", req.AvailableFrom)
	if err != nil {
		utils.WriteError(w, "available_from must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	toDate, err := time.Parse("2006-01-02", req.AvailableTo)
	if err != nil {
		utils.WriteError(w, "available_to must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if !toDate.After(fromDate) {
		utils.WriteError(w, "available_to must be after available_from", http.StatusBadRequest)
		return
	}

	checkIn := "14:00"
	checkOut := "11:00"
	if req.CheckInTime != "" {
		if _, err := time.Parse("15:04", req.CheckInTime); err != nil {
			utils.WriteError(w, "check_in_time must be HH:MM", http.StatusBadRequest)
			return
		}
		checkIn = req.CheckInTime
	}
	if req.CheckOutTime != "" {
		if _, err := time.Parse("15:04", req.CheckOutTime); err != nil {
			utils.WriteError(w, "check_out_time must be HH:MM", http.StatusBadRequest)
			return
		}
		checkOut = req.CheckOutTime
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ownerID uuid.UUID
	err = db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1 AND deleted_at IS NULL`, propID).Scan(&ownerID)
	if err != nil || ownerID != userID {
		utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
		return
	}

	var av shortlet.PropertyAvailability
	err = db.QueryRow(ctx, `
		INSERT INTO property_availability
		    (property_id, available_from, available_to, check_in_time, check_out_time, is_active)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		ON CONFLICT DO NOTHING
		RETURNING id, property_id,
		          available_from::TEXT, available_to::TEXT,
		          check_in_time::TEXT, check_out_time::TEXT,
		          is_active, created_at, updated_at
	`, propID, req.AvailableFrom, req.AvailableTo, checkIn, checkOut,
	).Scan(
		&av.ID, &av.PropertyID,
		&av.AvailableFrom, &av.AvailableTo,
		&av.CheckInTime, &av.CheckOutTime,
		&av.IsActive, &av.CreatedAt, &av.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		err = db.QueryRow(ctx, `
			UPDATE property_availability
			SET available_to = $3, check_in_time = $4, check_out_time = $5,
			    is_active = TRUE, updated_at = NOW()
			WHERE property_id = $1 AND available_from = $2
			RETURNING id, property_id,
			          available_from::TEXT, available_to::TEXT,
			          check_in_time::TEXT, check_out_time::TEXT,
			          is_active, created_at, updated_at
		`, propID, req.AvailableFrom, req.AvailableTo, checkIn, checkOut,
		).Scan(
			&av.ID, &av.PropertyID,
			&av.AvailableFrom, &av.AvailableTo,
			&av.CheckInTime, &av.CheckOutTime,
			&av.IsActive, &av.CreatedAt, &av.UpdatedAt,
		)
	}
	if err != nil {
		utils.Logger.Errorf("failed to upsert availability: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{
		"status":       "success",
		"message":      "availability window saved",
		"availability": av,
	})
}

// ============================================================================
// DELETE /properties/{id}/availability/{avail_id}
// ============================================================================

// DeletePropertyAvailability godoc
// @Summary      Remove an availability window
// @Description  Deactivates an availability window. Does not affect existing confirmed orders.
// @Tags         Properties / Availability
// @Produce      json
// @Param        id        path  string  true  "Property UUID"
// @Param        avail_id  path  string  true  "Availability UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/availability/{avail_id} [delete]
// @Security     BearerAuth
func DeletePropertyAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	propID, _ := uuid.Parse(r.PathValue("id"))
	availID, err := uuid.Parse(r.PathValue("avail_id"))
	if err != nil {
		utils.WriteError(w, "invalid availability id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result, err := db.Exec(ctx, `
		UPDATE property_availability SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1 AND property_id = $2
		  AND property_id IN (SELECT id FROM properties WHERE owner_id = $3)
	`, availID, propID, userID)
	if err != nil || result.RowsAffected() == 0 {
		utils.WriteError(w, "availability window not found", http.StatusNotFound)
		return
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "availability window removed"})
}

// ============================================================================
// POST /properties/{id}/availability/block
// ============================================================================

// BlockPropertyDate godoc
// @Summary      Block a specific date
// @Description  Adds a date-level override that makes a day unavailable (e.g. personal use, maintenance). Cannot block a date that has a confirmed booking.
// @Tags         Properties / Availability
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Property UUID"
// @Param        body  body  object{date=string,reason=string}  true  "date in YYYY-MM-DD"
// @Success 200 {object} PropertyAvailabilityOverrideResponse
// @Failure      400  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /properties/{id}/availability/block [post]
// @Security     BearerAuth
func BlockPropertyDate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
		utils.WriteError(w, "only owners can block dates", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	type request struct {
		Date   string  `json:"date"`
		Reason *string `json:"reason,omitempty"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Date == "" {
		utils.WriteError(w, "date is required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	parsed, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		utils.WriteError(w, "date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if parsed.Before(time.Now().Truncate(24 * time.Hour)) {
		utils.WriteError(w, "cannot block a past date", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ownerID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1`, propID).Scan(&ownerID); err != nil || ownerID != userID {
		utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
		return
	}

	var conflictCount int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM orders
		WHERE property_id = $1 AND status IN ('confirmed','checked_in')
		  AND check_in_date <= $2 AND check_out_date > $2
	`, propID, req.Date).Scan(&conflictCount)
	if conflictCount > 0 {
		utils.WriteError(w, "cannot block this date — it has a confirmed booking", http.StatusConflict)
		return
	}

	var ov shortlet.PropertyAvailabilityOverride
	err = db.QueryRow(ctx, `
		INSERT INTO property_availability_overrides (property_id, blocked_date, reason)
		VALUES ($1, $2, $3)
		ON CONFLICT (property_id, blocked_date)
		DO UPDATE SET reason = EXCLUDED.reason
		RETURNING id, property_id, blocked_date::TEXT, reason, created_at
	`, propID, req.Date, req.Reason).Scan(
		&ov.ID, &ov.PropertyID, &ov.BlockedDate, &ov.Reason, &ov.CreatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to block date: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "date blocked successfully",
		"override": ov,
	})
}

// ============================================================================
// DELETE /properties/{id}/availability/block/{date}
// ============================================================================

// UnblockPropertyDate godoc
// @Summary      Unblock a specific date
// @Description  Removes a previously blocked date override, making the day available again (subject to the active availability windows).
// @Tags         Properties / Availability
// @Produce      json
// @Param        id    path  string  true  "Property UUID"
// @Param        date  path  string  true  "Date in YYYY-MM-DD"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/availability/block/{date} [delete]
// @Security     BearerAuth
func UnblockPropertyDate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	propID, _ := uuid.Parse(r.PathValue("id"))
	dateStr := r.PathValue("date")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result, err := db.Exec(ctx, `
		DELETE FROM property_availability_overrides
		WHERE property_id = $1 AND blocked_date = $2
		  AND property_id IN (SELECT id FROM properties WHERE owner_id = $3)
	`, propID, dateStr, userID)
	if err != nil || result.RowsAffected() == 0 {
		utils.WriteError(w, "blocked date not found", http.StatusNotFound)
		return
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "date unblocked"})
}

// ============================================================================
// GET /properties/{id}/calendar  — calendar view
// ============================================================================

// GetPropertyCalendar godoc
// @Summary      Get property calendar
// @Description  Returns a day-by-day availability breakdown for a property over a date range (max 90 days). Each day indicates whether it is available, blocked by the owner, or already booked. Designed to power a calendar/date-picker UI.
// @Tags         Properties / Availability
// @Produce      json
// @Param        id    path   string  true  "Property UUID"
// @Param        from  query  string  true  "Start date YYYY-MM-DD (inclusive)"
// @Param        to    query  string  true  "End date YYYY-MM-DD (inclusive, max 90 days from from)"
// @Success 200 {object} PropertyCalendarResponse
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/calendar [get]
func GetPropertyCalendar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		utils.WriteError(w, "from and to query params are required (YYYY-MM-DD)", http.StatusBadRequest)
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
	if to.Sub(from) > 90*24*time.Hour {
		utils.WriteError(w, "date range cannot exceed 90 days", http.StatusBadRequest)
		return
	}

	cacheKey := shortletcache.KeyCalendar(propID.String(), fromStr, toStr)
	type calResp struct {
		Status       string                 `json:"status"`
		Count        int                    `json:"count"`
		CheckInTime  string                 `json:"check_in_time"`
		CheckOutTime string                 `json:"check_out_time"`
		Data         []shortlet.CalendarDay `json:"data"`
	}
	var cached calResp
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
		utils.WriteJSON(w, cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var checkInTime, checkOutTime string
	err = db.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT check_in_time::TEXT FROM property_availability
			 WHERE property_id = $1 AND is_active = TRUE LIMIT 1),
			'14:00'
		),
		COALESCE(
			(SELECT check_out_time::TEXT FROM property_availability
			 WHERE property_id = $1 AND is_active = TRUE LIMIT 1),
			'11:00'
		)
		FROM properties WHERE id = $1 AND deleted_at IS NULL
	`, propID).Scan(&checkInTime, &checkOutTime)
	if err != nil {
		utils.WriteError(w, "property not found", http.StatusNotFound)
		return
	}

	type window struct{ from, to time.Time }
	var windows []window
	wRows, _ := db.Query(ctx, `
		SELECT available_from, available_to
		FROM property_availability
		WHERE property_id = $1 AND is_active = TRUE
		  AND available_from <= $3 AND available_to >= $2
	`, propID, fromStr, toStr)
	if wRows != nil {
		defer wRows.Close()
		for wRows.Next() {
			var w window
			var fs, ts string
			wRows.Scan(&fs, &ts)
			w.from, _ = time.Parse("2006-01-02", fs)
			w.to, _ = time.Parse("2006-01-02", ts)
			windows = append(windows, w)
		}
	}

	blockedSet := make(map[string]bool)
	bRows, _ := db.Query(ctx, `
		SELECT blocked_date::TEXT FROM property_availability_overrides
		WHERE property_id = $1 AND blocked_date BETWEEN $2 AND $3
	`, propID, fromStr, toStr)
	if bRows != nil {
		defer bRows.Close()
		for bRows.Next() {
			var d string
			bRows.Scan(&d)
			blockedSet[d] = true
		}
	}

	type bookedRange struct{ from, to time.Time }
	var bookedRanges []bookedRange
	oRows, _ := db.Query(ctx, `
		SELECT check_in_date::TEXT, check_out_date::TEXT
		FROM orders
		WHERE property_id = $1
		  AND status IN ('confirmed','checked_in','pending')
		  AND check_in_date < $3 AND check_out_date > $2
	`, propID, fromStr, toStr)
	if oRows != nil {
		defer oRows.Close()
		for oRows.Next() {
			var cs, co string
			oRows.Scan(&cs, &co)
			cf, _ := time.Parse("2006-01-02", cs)
			ct, _ := time.Parse("2006-01-02", co)
			bookedRanges = append(bookedRanges, bookedRange{cf, ct})
		}
	}

	isInWindow := func(d time.Time) bool {
		for _, w := range windows {
			if !d.Before(w.from) && !d.After(w.to) {
				return true
			}
		}
		return false
	}

	isBooked := func(d time.Time) bool {
		for _, br := range bookedRanges {
			if !d.Before(br.from) && d.Before(br.to) {
				return true
			}
		}
		return false
	}

	days := make([]shortlet.CalendarDay, 0)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		blocked := blockedSet[dateStr]
		booked := isBooked(d)
		inWindow := isInWindow(d)
		available := inWindow && !blocked && !booked

		day := shortlet.CalendarDay{
			Date:      dateStr,
			Available: available,
			Blocked:   blocked,
			Booked:    booked,
		}
		if available {
			day.CheckInTime = checkInTime
			day.CheckOutTime = checkOutTime
		}
		days = append(days, day)
	}

	result := map[string]interface{}{
		"status":         "success",
		"count":          len(days),
		"check_in_time":  checkInTime,
		"check_out_time": checkOutTime,
		"data":           days,
	}

	go shortletcache.SetCached(context.Background(), cacheKey, result, shortletcache.TTLCalendar)

	utils.WriteJSON(w, result)
}

// ============================================================================
// POST /properties/{id}/save  — toggle saved/unsaved
// ============================================================================

// ToggleSavedListing godoc
// @Summary      Save or unsave a property
// @Description  Toggles the saved/bookmark status for a property. If not saved, it saves it. If already saved, it removes it. Only clients can save listings.
// @Tags         Saved Listings
// @Produce      json
// @Param        id  path  string  true  "Property UUID"
// @Success      200  {object}  object{status=string,message=string,saved=bool}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/save [post]
// @Security     BearerAuth
func ToggleSavedListing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "client" {
		utils.WriteError(w, "only clients can save listings", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var exists bool
	db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM properties WHERE id = $1 AND status = 'active' AND deleted_at IS NULL)`, propID).Scan(&exists)
	if !exists {
		utils.WriteError(w, "property not found", http.StatusNotFound)
		return
	}

	var alreadySaved bool
	db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saved_listings WHERE client_id = $1 AND property_id = $2)`, userID, propID).Scan(&alreadySaved)

	if alreadySaved {
		db.Exec(ctx, `DELETE FROM saved_listings WHERE client_id = $1 AND property_id = $2`, userID, propID)
		go shortletcache.InvalidateSavedListings(context.Background(), userID.String())
		utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "listing removed from saved", "saved": false})
	} else {
		db.Exec(ctx, `INSERT INTO saved_listings (client_id, property_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, propID)
		go shortletcache.InvalidateSavedListings(context.Background(), userID.String())
		utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "listing saved", "saved": true})
	}
}

// ============================================================================
// GET /clients/me/saved-listings
// ============================================================================

// GetSavedListings godoc
// @Summary      Get saved / bookmarked properties
// @Description  Returns a paginated list of properties that the authenticated client has saved.
// @Tags         Saved Listings
// @Produce      json
// @Param        page   query  integer false  "Page (default 1)"
// @Param        limit  query  integer false  "Items per page (default 20)"
// @Success 200 {object} SavedListingsResponse
// @Failure      403  {object}  object{error=string}
// @Router       /clients/me/saved-listings [get]
// @Security     BearerAuth
func GetSavedListings(w http.ResponseWriter, r *http.Request) {
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
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "client" {
		utils.WriteError(w, "only clients can view saved listings", http.StatusForbidden)
		return
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	cacheKey := shortletcache.KeySavedListings(userID.String(), page, limit)
	type savedResp struct {
		Status     string              `json:"status"`
		Count      int                 `json:"count"`
		Data       []shortlet.Property `json:"data"`
		Pagination map[string]int      `json:"pagination"`
	}
	var cached savedResp
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
		utils.WriteJSON(w, cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var total int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM saved_listings sl
		JOIN properties p ON p.id = sl.property_id
		WHERE sl.client_id = $1 AND p.deleted_at IS NULL
	`, userID).Scan(&total)

	rows, err := db.Query(ctx, `
		SELECT p.id, p.owner_id, p.name, p.description, p.property_type, p.status,
		       p.price_per_night, p.caution_fee,
		       p.images, p.amenities, p.house_rules,
		       p.max_adults, p.max_children,
		       p.state, p.city, p.street,
		       ST_Y(p.location::geometry), ST_X(p.location::geometry),
		       p.avg_rating, p.review_count, p.created_at, p.updated_at
		FROM saved_listings sl
		JOIN properties p ON p.id = sl.property_id
		WHERE sl.client_id = $1 AND p.deleted_at IS NULL AND p.status = 'active'
		ORDER BY sl.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	properties := make([]shortlet.Property, 0)
	for rows.Next() {
		var p shortlet.Property
		var ir, ar, rr []byte
		rows.Scan(
			&p.ID, &p.OwnerID, &p.Name, &p.Description,
			&p.PropertyType, &p.Status,
			&p.PricePerNight, &p.CautionFee,
			&ir, &ar, &rr,
			&p.MaxAdults, &p.MaxChildren,
			&p.State, &p.City, &p.Street,
			&p.Latitude, &p.Longitude,
			&p.AvgRating, &p.ReviewCount,
			&p.CreatedAt, &p.UpdatedAt,
		)
		json.Unmarshal(ir, &p.Images)
		json.Unmarshal(ar, &p.Amenities)
		json.Unmarshal(rr, &p.HouseRules)
		properties = append(properties, p)
	}

	totalPages := (total + limit - 1) / limit
	result := map[string]interface{}{
		"status": "success",
		"count":  len(properties),
		"data":   properties,
		"pagination": map[string]int{
			"total": total, "page": page,
			"limit": limit, "total_pages": totalPages,
		},
	}

	go shortletcache.SetCached(context.Background(), cacheKey, result, shortletcache.TTLSavedListings)

	utils.WriteJSON(w, result)
}
