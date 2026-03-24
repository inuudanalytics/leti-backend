package artisanprofilesettings

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	ID          uuid.UUID `json:"id"`
	ArtisanID   uuid.UUID `json:"artisan_id"`
	CategoryID  uuid.UUID `json:"category_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	BasePrice   float64   `json:"base_price"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ServiceOption struct {
	ID              uuid.UUID `json:"id"`
	ServiceID       uuid.UUID `json:"service_id"`
	VariationTypeID int       `json:"variation_type_id"`
	VariationLabel  string    `json:"variation_type_label"`
	Label           string    `json:"label"`
	PriceModifier   float64   `json:"price_modifier"`
	CreatedAt       time.Time `json:"created_at"`
}

type VariationType struct {
	ID         int    `json:"id"`
	CategoryID int    `json:"category_id"`
	Label      string `json:"label"`
}

// ============================================================================
// GET /categories/{id}/variation-types
// ============================================================================
// Public — returns the platform-defined variation types for a category.
// The frontend uses this to render the "add option" form for artisans.

// GetVariationTypes godoc
// @Summary      Get variation types for a category
// @Description  Returns the platform-defined variation types for a job category. Artisans use these to configure their service pricing options.
// @Tags         Artisan Services
// @Produce      json
// @Param        id  path  int  true  "Category ID"
// @Success      200  {object}  object{status=string,count=int,data=[]VariationType}
// @Failure      400  {object}  object{error=string}
// @Router       /categories/{id}/variation-types [get]
func GetVariationTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	categoryID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT id, category_id, label
		FROM service_variation_types
		WHERE category_id = $1
		ORDER BY id ASC
	`, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch variation types: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]VariationType, 0)
	for rows.Next() {
		var vt VariationType
		if err := rows.Scan(&vt.ID, &vt.CategoryID, &vt.Label); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		items = append(items, vt)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(items),
		"data":   items,
	})
}

// ============================================================================
// POST /artisan/services
// ============================================================================
// Artisan creates a new service listing under one of their registered categories.

// CreateService godoc
// @Summary      Create a service
// @Description  Artisan creates a new service listing under one of their registered categories. The artisan must already have the category registered in artisan_categories.
// @Tags         Artisan Services
// @Accept       json
// @Produce      json
// @Param        body  body  object{category_id=int,name=string,description=string,base_price=number}  true  "Service details"
// @Success      201   {object}  object{status=string,message=string,data=Service}
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /artisan/services [post]
// @Security     BearerAuth
func CreateService(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can create services", http.StatusForbidden)
		return
	}

	type request struct {
		CategoryID  uuid.UUID `json:"category_id"`
		Name        string    `json:"name"`
		Description *string   `json:"description,omitempty"`
		BasePrice   float64   `json:"base_price"`
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
	if req.Name == "" {
		utils.WriteError(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.BasePrice < 0 {
		utils.WriteError(w, "base_price cannot be negative", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Artisan must have this category registered
	var catExists bool
	_ = db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM artisan_categories
			WHERE artisan_id = $1 AND category_id = $2
		)
	`, userID, req.CategoryID).Scan(&catExists)
	if !catExists {
		utils.WriteError(w, "you do not have this category registered — add it to your profile first", http.StatusBadRequest)
		return
	}

	var svc Service
	err := db.QueryRow(ctx, `
		INSERT INTO artisan_services (artisan_id, category_id, name, description, base_price)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, artisan_id, category_id, name, description,
		          base_price, is_active, created_at, updated_at
	`, userID, req.CategoryID, req.Name, req.Description, req.BasePrice).Scan(
		&svc.ID, &svc.ArtisanID, &svc.CategoryID, &svc.Name, &svc.Description,
		&svc.BasePrice, &svc.IsActive, &svc.CreatedAt, &svc.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			utils.WriteError(w, "you already have a service with this name in this category", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to create service: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "service created",
		"data":    svc,
	})
}

// ============================================================================
// GET /artisan/services
// ============================================================================
// Artisan views their own services, optionally filtered by category.

// GetMyServices godoc
// @Summary      Get own services
// @Description  Returns all services created by the authenticated artisan, optionally filtered by category_id.
// @Tags         Artisan Services
// @Produce      json
// @Param        category_id  query  int  false  "Filter by category"
// @Success      200  {object}  object{status=string,count=int,data=[]Service}
// @Router       /artisan/services [get]
// @Security     BearerAuth
func GetMyServices(w http.ResponseWriter, r *http.Request) {
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

	args := []interface{}{userID}
	query := `
		SELECT id, artisan_id, category_id, name, description,
		       base_price, is_active, created_at, updated_at
		FROM artisan_services
		WHERE artisan_id = $1`

	if catStr := r.URL.Query().Get("category_id"); catStr != "" {
		catID, err := strconv.Atoi(catStr)
		if err != nil {
			utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
			return
		}
		query += " AND category_id = $2"
		args = append(args, catID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		utils.Logger.Errorf("failed to fetch services: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	services := make([]Service, 0)
	for rows.Next() {
		var svc Service
		if err := rows.Scan(
			&svc.ID, &svc.ArtisanID, &svc.CategoryID, &svc.Name, &svc.Description,
			&svc.BasePrice, &svc.IsActive, &svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		services = append(services, svc)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(services),
		"data":   services,
	})
}

// ============================================================================
// GET /artisans/{id}/services
// ============================================================================
// Public — clients browse an artisan's active services before booking.

// GetArtisanServices godoc
// @Summary      Get an artisan's services (public)
// @Description  Returns all active services for a specific artisan. Clients use this to browse before creating a booking.
// @Tags         Artisan Services
// @Produce      json
// @Param        id           path   string  true   "Artisan UUID"
// @Param        category_id  query  int     false  "Filter by category"
// @Success      200  {object}  object{status=string,count=int,data=[]object}
// @Failure      404  {object}  object{error=string}
// @Router       /artisans/{id}/services [get]
func GetArtisanServices(w http.ResponseWriter, r *http.Request) {
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

	args := []interface{}{artisanID}
	query := `
		SELECT s.id, s.artisan_id, s.category_id, s.name, s.description,
		       s.base_price, s.is_active, s.created_at, s.updated_at
		FROM artisan_services s
		WHERE s.artisan_id = $1 AND s.is_active = TRUE`

	if catStr := r.URL.Query().Get("category_id"); catStr != "" {
		catID, err := strconv.Atoi(catStr)
		if err != nil {
			utils.WriteError(w, "invalid category_id", http.StatusBadRequest)
			return
		}
		query += " AND s.category_id = $2"
		args = append(args, catID)
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := db.Query(r.Context(), query, args...)
	if err != nil {
		utils.Logger.Errorf("failed to fetch artisan services: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	services := make([]Service, 0)
	for rows.Next() {
		var svc Service
		if err := rows.Scan(
			&svc.ID, &svc.ArtisanID, &svc.CategoryID, &svc.Name, &svc.Description,
			&svc.BasePrice, &svc.IsActive, &svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		services = append(services, svc)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(services),
		"data":   services,
	})
}

// ============================================================================
// PATCH /artisan/services/{id}
// ============================================================================

// UpdateService godoc
// @Summary      Update a service
// @Description  Artisan updates the name, description, base price, or active status of one of their services.
// @Tags         Artisan Services
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Service UUID"
// @Param        body  body  object{name=string,description=string,base_price=number,is_active=bool}  true  "Fields to update (all optional)"
// @Success      200   {object}  object{status=string,message=string,data=Service}
// @Failure      404   {object}  object{error=string}
// @Router       /artisan/services/{id} [patch]
// @Security     BearerAuth
func UpdateService(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can update services", http.StatusForbidden)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}

	type request struct {
		Name        *string  `json:"name,omitempty"`
		Description *string  `json:"description,omitempty"`
		BasePrice   *float64 `json:"base_price,omitempty"`
		IsActive    *bool    `json:"is_active,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Name == nil && req.Description == nil && req.BasePrice == nil && req.IsActive == nil {
		utils.WriteError(w, "nothing to update — provide at least one field", http.StatusBadRequest)
		return
	}
	if req.Name != nil && *req.Name == "" {
		utils.WriteError(w, "name cannot be empty", http.StatusBadRequest)
		return
	}
	if req.BasePrice != nil && *req.BasePrice < 0 {
		utils.WriteError(w, "base_price cannot be negative", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch current values to apply partial update
	var cur Service
	err = db.QueryRow(ctx, `
		SELECT id, artisan_id, category_id, name, description,
		       base_price, is_active, created_at, updated_at
		FROM artisan_services
		WHERE id = $1 AND artisan_id = $2
	`, serviceID, userID).Scan(
		&cur.ID, &cur.ArtisanID, &cur.CategoryID, &cur.Name, &cur.Description,
		&cur.BasePrice, &cur.IsActive, &cur.CreatedAt, &cur.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "service not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch service: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Apply partial updates
	if req.Name != nil {
		cur.Name = *req.Name
	}
	if req.Description != nil {
		cur.Description = req.Description
	}
	if req.BasePrice != nil {
		cur.BasePrice = *req.BasePrice
	}
	if req.IsActive != nil {
		cur.IsActive = *req.IsActive
	}

	var updated Service
	err = db.QueryRow(ctx, `
		UPDATE artisan_services
		SET name        = $1,
		    description = $2,
		    base_price  = $3,
		    is_active   = $4
		WHERE id = $5 AND artisan_id = $6
		RETURNING id, artisan_id, category_id, name, description,
		          base_price, is_active, created_at, updated_at
	`, cur.Name, cur.Description, cur.BasePrice, cur.IsActive,
		serviceID, userID).Scan(
		&updated.ID, &updated.ArtisanID, &updated.CategoryID, &updated.Name, &updated.Description,
		&updated.BasePrice, &updated.IsActive, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			utils.WriteError(w, "you already have a service with this name in this category", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to update service: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "service updated",
		"data":    updated,
	})
}

// ============================================================================
// DELETE /artisan/services/{id}
// ============================================================================

// DeleteService godoc
// @Summary      Delete a service
// @Description  Permanently deletes a service and all its options. Will fail if the service is referenced by a confirmed or pending booking.
// @Tags         Artisan Services
// @Produce      json
// @Param        id  path  string  true  "Service UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /artisan/services/{id} [delete]
// @Security     BearerAuth
func DeleteService(w http.ResponseWriter, r *http.Request) {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "artisan" {
		utils.WriteError(w, "only artisans can delete services", http.StatusForbidden)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Block deletion if there are active bookings referencing this service
	var activeBookings int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM artisan_bookings
		WHERE service_id = $1
		  AND status IN ('pending', 'confirmed')
	`, serviceID).Scan(&activeBookings)
	if activeBookings > 0 {
		utils.WriteError(w, "cannot delete a service with active bookings — cancel them first", http.StatusConflict)
		return
	}

	result, err := db.Exec(ctx, `
		DELETE FROM artisan_services WHERE id = $1 AND artisan_id = $2
	`, serviceID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to delete service: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "service not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "service deleted",
	})
}

// ============================================================================
// POST /artisan/services/{id}/options
// ============================================================================
// Artisan adds a pricing option to one of their services.
// One option per variation_type_id+label combination per service.

// AddServiceOption godoc
// @Summary      Add a pricing option to a service
// @Description  Artisan adds a variation option to their service. Each option references a platform variation type (e.g. "Hair length") and carries the artisan's own label (e.g. "Extra long") and a price modifier on top of the base price.
// @Tags         Artisan Services
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Service UUID"
// @Param        body  body  object{variation_type_id=int,label=string,price_modifier=number}  true  "Option details"
// @Success      201   {object}  object{status=string,message=string,data=ServiceOption}
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /artisan/services/{id}/options [post]
// @Security     BearerAuth
func AddServiceOption(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can add service options", http.StatusForbidden)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}

	type request struct {
		VariationTypeID int     `json:"variation_type_id"`
		Label           string  `json:"label"`
		PriceModifier   float64 `json:"price_modifier"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.VariationTypeID == 0 {
		utils.WriteError(w, "variation_type_id is required", http.StatusBadRequest)
		return
	}
	if req.Label == "" {
		utils.WriteError(w, "label is required", http.StatusBadRequest)
		return
	}
	if req.PriceModifier < 0 {
		utils.WriteError(w, "price_modifier cannot be negative", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Verify service belongs to this artisan and is active
	var categoryID int
	err = db.QueryRow(ctx, `
		SELECT category_id FROM artisan_services
		WHERE id = $1 AND artisan_id = $2 AND is_active = TRUE
	`, serviceID, userID).Scan(&categoryID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "service not found or not active", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Verify the variation type belongs to this service's category
	var vtExists bool
	_ = db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM service_variation_types
			WHERE id = $1 AND category_id = $2
		)
	`, req.VariationTypeID, categoryID).Scan(&vtExists)
	if !vtExists {
		utils.WriteError(w, "variation_type_id does not belong to this service's category", http.StatusBadRequest)
		return
	}

	var opt ServiceOption
	var vtLabel string
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_service_options (service_id, variation_type_id, label, price_modifier)
		VALUES ($1, $2, $3, $4)
		RETURNING id, service_id, variation_type_id, label, price_modifier, created_at
	`, serviceID, req.VariationTypeID, req.Label, req.PriceModifier).Scan(
		&opt.ID, &opt.ServiceID, &opt.VariationTypeID, &opt.Label,
		&opt.PriceModifier, &opt.CreatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			utils.WriteError(w, "an option with this label already exists for this variation type on this service", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to insert service option: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Fetch variation type label for the response
	_ = db.QueryRow(ctx, `
		SELECT label FROM service_variation_types WHERE id = $1
	`, req.VariationTypeID).Scan(&vtLabel)
	opt.VariationLabel = vtLabel

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "option added",
		"data":    opt,
	})
}

// ============================================================================
// GET /artisan/services/{id}/options
// ============================================================================

// GetServiceOptions godoc
// @Summary      Get options for a service
// @Description  Returns all pricing options configured for a service, grouped by variation type. Works for both the artisan owner and public clients.
// @Tags         Artisan Services
// @Produce      json
// @Param        id  path  string  true  "Service UUID"
// @Success      200  {object}  object{status=string,count=int,data=[]object}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan/services/{id}/options [get]
// @Security     BearerAuth
func GetServiceOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}

	// Verify the service exists (public — no auth needed)
	var exists bool
	_ = db.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM artisan_services WHERE id = $1)
	`, serviceID).Scan(&exists)
	if !exists {
		utils.WriteError(w, "service not found", http.StatusNotFound)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT
			o.id,
			o.service_id,
			o.variation_type_id,
			vt.label  AS variation_type_label,
			o.label,
			o.price_modifier,
			o.created_at
		FROM artisan_service_options o
		JOIN service_variation_types vt ON vt.id = o.variation_type_id
		WHERE o.service_id = $1
		ORDER BY o.variation_type_id ASC, o.label ASC
	`, serviceID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch service options: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Group by variation type for a nicer response shape
	type OptionItem struct {
		ID            uuid.UUID `json:"id"`
		Label         string    `json:"label"`
		PriceModifier float64   `json:"price_modifier"`
		CreatedAt     time.Time `json:"created_at"`
	}
	type VariationGroup struct {
		VariationTypeID    int          `json:"variation_type_id"`
		VariationTypeLabel string       `json:"variation_type_label"`
		Options            []OptionItem `json:"options"`
	}

	groupMap := make(map[int]*VariationGroup)
	groupOrder := make([]int, 0)

	for rows.Next() {
		var opt ServiceOption
		if err := rows.Scan(
			&opt.ID, &opt.ServiceID, &opt.VariationTypeID, &opt.VariationLabel,
			&opt.Label, &opt.PriceModifier, &opt.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if _, seen := groupMap[opt.VariationTypeID]; !seen {
			groupMap[opt.VariationTypeID] = &VariationGroup{
				VariationTypeID:    opt.VariationTypeID,
				VariationTypeLabel: opt.VariationLabel,
				Options:            []OptionItem{},
			}
			groupOrder = append(groupOrder, opt.VariationTypeID)
		}
		groupMap[opt.VariationTypeID].Options = append(groupMap[opt.VariationTypeID].Options, OptionItem{
			ID:            opt.ID,
			Label:         opt.Label,
			PriceModifier: opt.PriceModifier,
			CreatedAt:     opt.CreatedAt,
		})
	}

	groups := make([]*VariationGroup, 0, len(groupOrder))
	for _, id := range groupOrder {
		groups = append(groups, groupMap[id])
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(groups),
		"data":   groups,
	})
}

// ============================================================================
// PATCH /artisan/services/{id}/options/{optionId}
// ============================================================================

// UpdateServiceOption godoc
// @Summary      Update a service option
// @Description  Artisan updates the label or price modifier of an existing service option.
// @Tags         Artisan Services
// @Accept       json
// @Produce      json
// @Param        id        path  string  true  "Service UUID"
// @Param        optionId  path  string  true  "Option UUID"
// @Param        body      body  object{label=string,price_modifier=number}  true  "Fields to update"
// @Success      200  {object}  object{status=string,message=string,data=ServiceOption}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan/services/{id}/options/{optionId} [patch]
// @Security     BearerAuth
func UpdateServiceOption(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can update service options", http.StatusForbidden)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}
	optionID, err := uuid.Parse(r.PathValue("optionId"))
	if err != nil {
		utils.WriteError(w, "invalid option id", http.StatusBadRequest)
		return
	}

	type request struct {
		Label         *string  `json:"label,omitempty"`
		PriceModifier *float64 `json:"price_modifier,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Label == nil && req.PriceModifier == nil {
		utils.WriteError(w, "nothing to update — provide label or price_modifier", http.StatusBadRequest)
		return
	}
	if req.Label != nil && *req.Label == "" {
		utils.WriteError(w, "label cannot be empty", http.StatusBadRequest)
		return
	}
	if req.PriceModifier != nil && *req.PriceModifier < 0 {
		utils.WriteError(w, "price_modifier cannot be negative", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Verify ownership: service must belong to this artisan
	var ownerCheck uuid.UUID
	err = db.QueryRow(ctx, `
		SELECT artisan_id FROM artisan_services WHERE id = $1
	`, serviceID).Scan(&ownerCheck)
	if err != nil || ownerCheck != userID {
		utils.WriteError(w, "service not found", http.StatusNotFound)
		return
	}

	// Fetch current option values for partial update
	var cur ServiceOption
	err = db.QueryRow(ctx, `
		SELECT id, service_id, variation_type_id, label, price_modifier, created_at
		FROM artisan_service_options
		WHERE id = $1 AND service_id = $2
	`, optionID, serviceID).Scan(
		&cur.ID, &cur.ServiceID, &cur.VariationTypeID,
		&cur.Label, &cur.PriceModifier, &cur.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "option not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if req.Label != nil {
		cur.Label = *req.Label
	}
	if req.PriceModifier != nil {
		cur.PriceModifier = *req.PriceModifier
	}

	var updated ServiceOption
	err = db.QueryRow(ctx, `
		UPDATE artisan_service_options
		SET label          = $1,
		    price_modifier = $2
		WHERE id = $3 AND service_id = $4
		RETURNING id, service_id, variation_type_id, label, price_modifier, created_at
	`, cur.Label, cur.PriceModifier, optionID, serviceID).Scan(
		&updated.ID, &updated.ServiceID, &updated.VariationTypeID,
		&updated.Label, &updated.PriceModifier, &updated.CreatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			utils.WriteError(w, "an option with this label already exists for this variation type", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to update option: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Fetch variation type label for the response
	_ = db.QueryRow(ctx, `
		SELECT label FROM service_variation_types WHERE id = $1
	`, updated.VariationTypeID).Scan(&updated.VariationLabel)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "option updated",
		"data":    updated,
	})
}

// ============================================================================
// DELETE /artisan/services/{id}/options/{optionId}
// ============================================================================

// DeleteServiceOption godoc
// @Summary      Delete a service option
// @Description  Removes a pricing option from a service.
// @Tags         Artisan Services
// @Produce      json
// @Param        id        path  string  true  "Service UUID"
// @Param        optionId  path  string  true  "Option UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan/services/{id}/options/{optionId} [delete]
// @Security     BearerAuth
func DeleteServiceOption(w http.ResponseWriter, r *http.Request) {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "artisan" {
		utils.WriteError(w, "only artisans can delete service options", http.StatusForbidden)
		return
	}

	serviceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid service id", http.StatusBadRequest)
		return
	}
	optionID, err := uuid.Parse(r.PathValue("optionId"))
	if err != nil {
		utils.WriteError(w, "invalid option id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Verify service ownership
	var ownerCheck uuid.UUID
	err = db.QueryRow(ctx, `
		SELECT artisan_id FROM artisan_services WHERE id = $1
	`, serviceID).Scan(&ownerCheck)
	if err != nil || ownerCheck != userID {
		utils.WriteError(w, "service not found", http.StatusNotFound)
		return
	}

	result, err := db.Exec(ctx, `
		DELETE FROM artisan_service_options WHERE id = $1 AND service_id = $2
	`, optionID, serviceID)
	if err != nil {
		utils.Logger.Errorf("failed to delete option: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "option not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "option deleted",
	})
}

// ============================================================================
// helpers
// ============================================================================

func isPgUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgconn.PgError.Code "23505" = unique_violation
	type pgErr interface{ SQLState() string }
	if e, ok := err.(pgErr); ok {
		return e.SQLState() == "23505"
	}
	return false
}
