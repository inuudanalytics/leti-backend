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
)

type PropertyAvailabilityOverrideResponse struct {
	Status   string                                `json:"status"`
	Message  string                                `json:"message"`
	Override shortlet.PropertyAvailabilityOverride `json:"override"`
}

type PropertyCalendarResponse struct {
	Status string                 `json:"status"`
	Count  int                    `json:"count"`
	Data   []shortlet.CalendarDay `json:"data"`
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
// POST /properties/{id}/availability/block
// ============================================================================

// BlockPropertyDate godoc
// @Summary      Block a specific date
// @Description  Marks a specific date as unavailable on the property calendar (e.g. personal use, maintenance). All dates are available by default; owners only need to explicitly block ones they cannot accept bookings for. Cannot block a date that already has a confirmed or checked-in booking.
// @Tags         Properties / Availability
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Property UUID"
// @Param        body  body  object{date=string,reason=string}  true  "date in YYYY-MM-DD, reason optional"
// @Success      200  {object}  PropertyAvailabilityOverrideResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
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
	if err := db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1 AND deleted_at IS NULL`, propID).Scan(&ownerID); err != nil || ownerID != userID {
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
// POST /properties/{id}/availability/block-range
// ============================================================================

// BlockPropertyDateRange godoc
// @Summary      Block a range of dates
// @Description  Marks every date from `from` to `to` inclusive as unavailable. Dates that already have confirmed or checked-in bookings are skipped and reported separately — they do NOT cause the whole request to fail. Past dates are also skipped.
// @Tags         Properties / Availability
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Property UUID"
// @Param        body  body  object{from=string,to=string,reason=string}  true  "from/to in YYYY-MM-DD (max 365-day range), reason optional"
// @Success      200  {object}  object{status=string,message=string,blocked=[]string,skipped=[]string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/availability/block-range [post]
// @Security     BearerAuth
func BlockPropertyDateRange(w http.ResponseWriter, r *http.Request) {
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
		From   string  `json:"from"`
		To     string  `json:"to"`
		Reason *string `json:"reason,omitempty"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.From == "" || req.To == "" {
		utils.WriteError(w, "from and to are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	fromDate, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		utils.WriteError(w, "from must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	toDate, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		utils.WriteError(w, "to must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if toDate.Before(fromDate) {
		utils.WriteError(w, "to must be on or after from", http.StatusBadRequest)
		return
	}
	if toDate.Sub(fromDate) > 365*24*time.Hour {
		utils.WriteError(w, "range cannot exceed 365 days", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var ownerID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1 AND deleted_at IS NULL`, propID).Scan(&ownerID); err != nil || ownerID != userID {
		utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
		return
	}

	bookedSet := make(map[string]bool)
	bkRows, _ := db.Query(ctx, `
		SELECT check_in_date::TEXT, check_out_date::TEXT FROM orders
		WHERE property_id = $1 AND status IN ('confirmed','checked_in')
		  AND check_in_date <= $3 AND check_out_date > $2
	`, propID, req.From, req.To)
	if bkRows != nil {
		defer bkRows.Close()
		for bkRows.Next() {
			var cs, co string
			bkRows.Scan(&cs, &co)
			cf, _ := time.Parse("2006-01-02", cs)
			ct, _ := time.Parse("2006-01-02", co)
			for d := cf; d.Before(ct); d = d.AddDate(0, 0, 1) {
				bookedSet[d.Format("2006-01-02")] = true
			}
		}
	}

	today := time.Now().Truncate(24 * time.Hour)
	blocked := make([]string, 0)
	skipped := make([]string, 0)

	for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if d.Before(today) || bookedSet[ds] {
			skipped = append(skipped, ds)
			continue
		}
		_, execErr := db.Exec(ctx, `
			INSERT INTO property_availability_overrides (property_id, blocked_date, reason)
			VALUES ($1, $2, $3)
			ON CONFLICT (property_id, blocked_date) DO UPDATE SET reason = EXCLUDED.reason
		`, propID, ds, req.Reason)
		if execErr != nil {
			utils.Logger.Errorf("failed to block date %s: %v", ds, execErr)
			skipped = append(skipped, ds)
		} else {
			blocked = append(blocked, ds)
		}
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "date range processed",
		"blocked": blocked,
		"skipped": skipped,
	})
}

// ============================================================================
// DELETE /properties/{id}/availability/block/{date}
// ============================================================================

// UnblockPropertyDate godoc
// @Summary      Unblock a specific date
// @Description  Removes a previously blocked date, making it available for bookings again.
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
		  AND property_id IN (SELECT id FROM properties WHERE owner_id = $3 AND deleted_at IS NULL)
	`, propID, dateStr, userID)
	if err != nil || result.RowsAffected() == 0 {
		utils.WriteError(w, "blocked date not found", http.StatusNotFound)
		return
	}

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "message": "date unblocked"})
}

// ============================================================================
// GET /properties/{id}/blocked-dates  — owner management view
// ============================================================================

// GetBlockedDates godoc
// @Summary      List all blocked dates for a property (owner only)
// @Description  Returns all dates the owner has explicitly blocked. Clients should use GET /properties/{id}/calendar instead, which merges blocked dates and existing bookings into a single availability view.
// @Tags         Properties / Availability
// @Produce      json
// @Param        id    path   string  true  "Property UUID"
// @Param        from  query  string  false "Filter from date YYYY-MM-DD (inclusive)"
// @Param        to    query  string  false "Filter to date YYYY-MM-DD (inclusive)"
// @Success      200  {object}  object{status=string,count=int,data=[]shortlet.PropertyAvailabilityOverride}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/blocked-dates [get]
// @Security     BearerAuth
func GetBlockedDates(w http.ResponseWriter, r *http.Request) {
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
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "owner" {
		utils.WriteError(w, "only owners can view blocked dates", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ownerID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1 AND deleted_at IS NULL`, propID).Scan(&ownerID); err != nil || ownerID != userID {
		utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
		return
	}

	args := []interface{}{propID}
	where := "property_id = $1"
	argIdx := 2
	if from := r.URL.Query().Get("from"); from != "" {
		where += " AND blocked_date >= $2"
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to"); to != "" {
		where += " AND blocked_date <= $" + itoa(argIdx)
		args = append(args, to)
	}

	rows, err := db.Query(ctx, `
		SELECT id, property_id, blocked_date::TEXT, reason, created_at
		FROM property_availability_overrides
		WHERE `+where+`
		ORDER BY blocked_date ASC
	`, args...)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	overrides := make([]shortlet.PropertyAvailabilityOverride, 0)
	for rows.Next() {
		var ov shortlet.PropertyAvailabilityOverride
		rows.Scan(&ov.ID, &ov.PropertyID, &ov.BlockedDate, &ov.Reason, &ov.CreatedAt)
		overrides = append(overrides, ov)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(overrides),
		"data":   overrides,
	})
}

func itoa(n int) string {
	return string(rune('0' + n))
}

// ============================================================================
// GET /properties/{id}/calendar  — day-by-day availability (public)
// ============================================================================

// GetPropertyCalendar godoc
// @Summary      Get property calendar
// @Description  Returns a day-by-day availability breakdown for a property over a date range (max 90 days). Every date is available by default; it becomes unavailable when the owner has explicitly blocked it OR it falls inside an existing booking. Designed to power a date-picker UI.
// @Tags         Properties / Availability
// @Produce      json
// @Param        id    path   string  true  "Property UUID"
// @Param        from  query  string  true  "Start date YYYY-MM-DD (inclusive)"
// @Param        to    query  string  true  "End date YYYY-MM-DD (inclusive, max 90 days from from)"
// @Success      200  {object}  PropertyCalendarResponse
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
		Status string                 `json:"status"`
		Count  int                    `json:"count"`
		Data   []shortlet.CalendarDay `json:"data"`
	}
	var cached calResp
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
		utils.WriteJSON(w, cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM properties WHERE id = $1 AND deleted_at IS NULL)`, propID).Scan(&exists); err != nil || !exists {
		utils.WriteError(w, "property not found", http.StatusNotFound)
		return
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
		SELECT check_in_date::TEXT, check_out_date::TEXT FROM orders
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

	isBooked := func(d time.Time) bool {
		for _, br := range bookedRanges {
			if !d.Before(br.from) && d.Before(br.to) {
				return true
			}
		}
		return false
	}

	days := make([]shortlet.CalendarDay, 0, int(to.Sub(from).Hours()/24)+1)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		blocked := blockedSet[dateStr]
		booked := isBooked(d)
		days = append(days, shortlet.CalendarDay{
			Date:      dateStr,
			Available: !blocked && !booked,
			Blocked:   blocked,
			Booked:    booked,
		})
	}

	result := map[string]interface{}{
		"status": "success",
		"count":  len(days),
		"data":   days,
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
// @Success      200  {object}  SavedListingsResponse
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
