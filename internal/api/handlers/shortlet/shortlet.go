package shortlet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	shortletcache "leti_server/internal/api/handlers/shortlet/shortletcache"
	"leti_server/internal/models/shortlet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PropertyResponse struct {
	Status   string            `json:"status"`
	Message  string            `json:"message"`
	Property shortlet.Property `json:"property"`
}

type PropertyListResponse struct {
	Status     string              `json:"status"`
	Count      int                 `json:"count"`
	Data       []shortlet.Property `json:"data"`
	Pagination map[string]int      `json:"pagination"`
}

type PropertyDetailResponse struct {
	Status  string                 `json:"status"`
	Data    shortlet.Property      `json:"data"`
	IsSaved bool                   `json:"is_saved"`
	Owner   map[string]interface{} `json:"owner"`
}

// ============================================================================
// POST /properties/draft  — create an empty / partial draft listing
// ============================================================================

// CreatePropertyDraft godoc
// @Summary      Create a draft property listing
// @Description  Creates a draft listing that the owner can fill in incrementally and publish later. All fields are optional — save whatever the owner has typed so far. The listing will have status='draft' and will not appear in public searches until published.
// @Tags         Properties
// @Accept       mpfd
// @Produce      json
// @Param        name            formData  string  false  "Property name"
// @Param        description     formData  string  false  "Property description"
// @Param        property_type   formData  string  false  "Property type"
// @Param        price_per_night formData  number  false  "Nightly rate in NGN"
// @Param        caution_fee     formData  number  false  "Refundable caution fee"
// @Param        amenities       formData  string  false  "JSON array e.g. [\"WiFi\",\"AC\"]"
// @Param        house_rules     formData  string  false  "JSON array e.g. [\"No smoking\"]"
// @Param        max_adults      formData  integer false  "Max adult guests"
// @Param        max_children    formData  integer false  "Max children"
// @Param        state           formData  string  false  "Nigerian state"
// @Param        city            formData  string  false  "City"
// @Param        street          formData  string  false  "Street address"
// @Param        latitude        formData  number  false  "Latitude"
// @Param        longitude       formData  number  false  "Longitude"
// @Param        images          formData  file    false  "Images (max 5)"
// @Success 201 {object} PropertyResponse
// @Failure      403  {object}  object{error=string}
// @Router       /properties/draft [post]
// @Security     BearerAuth
func CreatePropertyDraft(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only property owners can create listings", http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		r.ParseForm()
	}

	draft := map[string]interface{}{}

	if v := strings.TrimSpace(r.FormValue("name")); v != "" {
		draft["name"] = v
	}
	if v := r.FormValue("description"); v != "" {
		draft["description"] = v
	}
	if v := r.FormValue("property_type"); v != "" {
		draft["property_type"] = v
	}
	if v := r.FormValue("price_per_night"); v != "" {
		var price float64
		fmt.Sscanf(v, "%f", &price)
		draft["price_per_night"] = price
	}
	if v := r.FormValue("caution_fee"); v != "" {
		var fee float64
		fmt.Sscanf(v, "%f", &fee)
		draft["caution_fee"] = fee
	}
	if v := r.FormValue("amenities"); v != "" {
		var arr []string
		if json.Unmarshal([]byte(v), &arr) == nil {
			draft["amenities"] = arr
		}
	}
	if v := r.FormValue("house_rules"); v != "" {
		var arr []string
		if json.Unmarshal([]byte(v), &arr) == nil {
			draft["house_rules"] = arr
		}
	}
	if v := r.FormValue("max_adults"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		draft["max_adults"] = n
	}
	if v := r.FormValue("max_children"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		draft["max_children"] = n
	}
	if v := r.FormValue("state"); v != "" {
		draft["state"] = v
	}
	if v := r.FormValue("city"); v != "" {
		draft["city"] = v
	}
	if v := r.FormValue("street"); v != "" {
		draft["street"] = v
	}
	if latStr, lngStr := r.FormValue("latitude"), r.FormValue("longitude"); latStr != "" && lngStr != "" {
		var lat, lng float64
		fmt.Sscanf(latStr, "%f", &lat)
		fmt.Sscanf(lngStr, "%f", &lng)
		draft["latitude"] = lat
		draft["longitude"] = lng
	}

	var uploadedImages []shortlet.PropertyImage
	if r.MultipartForm != nil && len(r.MultipartForm.File["images"]) > 0 {
		imageHeaders := r.MultipartForm.File["images"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 images allowed", http.StatusBadRequest)
			return
		}
		ctx60, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize image service", http.StatusInternalServerError)
			return
		}
		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()
		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx60, cloud, files, "properties")
		if err != nil {
			utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
			return
		}
		now := time.Now()
		for i, url := range urls {
			uploadedImages = append(uploadedImages, shortlet.PropertyImage{
				URL: url, PublicID: publicIDs[i], UpdatedAt: now,
			})
		}
		draft["images"] = uploadedImages
	}

	draftJSON, _ := json.Marshal(draft)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var prop shortlet.Property
	var imagesRaw, amenitiesRaw, rulesRaw []byte

	err := db.QueryRow(ctx, `
		INSERT INTO properties (
			owner_id, name, description, property_type,
			price_per_night, caution_fee,
			images, amenities, house_rules,
			max_adults, max_children,
			state, city, street,
			status, draft_data
		) VALUES (
			$1,
			COALESCE(NULLIF($2, ''), 'Untitled Draft'),
			$3, 'apartment',
			0, 0,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
			1, 0,
			COALESCE(NULLIF($4, ''), '—'),
			COALESCE(NULLIF($5, ''), '—'),
			COALESCE(NULLIF($6, ''), '—'),
			'draft', $7
		)
		RETURNING id, owner_id, name, description, property_type, status,
		          price_per_night, caution_fee,
		          images, amenities, house_rules,
		          max_adults, max_children,
		          state, city, street,
		          ST_Y(location::geometry) AS latitude,
		          ST_X(location::geometry) AS longitude,
		          avg_rating, review_count, created_at, updated_at
	`,
		userID,
		draft["name"],
		draft["description"],
		draft["state"],
		draft["city"],
		draft["street"],
		draftJSON,
	).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &prop.Status,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to create draft property: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	if v, ok := draft["name"].(string); ok && v != "" {
		prop.Name = v
	}
	if v, ok := draft["description"].(string); ok {
		prop.Description = &v
	}
	if v, ok := draft["property_type"].(string); ok && v != "" {
		prop.PropertyType = v
	}
	if v, ok := draft["price_per_night"].(float64); ok {
		prop.PricePerNight = v
	}
	if v, ok := draft["caution_fee"].(float64); ok {
		prop.CautionFee = v
	}
	if v, ok := draft["max_adults"].(int); ok {
		prop.MaxAdults = v
	}
	if v, ok := draft["max_children"].(int); ok {
		prop.MaxChildren = v
	}
	if v, ok := draft["state"].(string); ok && v != "" {
		prop.State = v
	}
	if v, ok := draft["city"].(string); ok && v != "" {
		prop.City = v
	}
	if v, ok := draft["street"].(string); ok && v != "" {
		prop.Street = v
	}
	if v, ok := draft["amenities"].([]string); ok {
		prop.Amenities = v
	}
	if v, ok := draft["house_rules"].([]string); ok {
		prop.HouseRules = v
	}
	if v, ok := draft["images"].([]shortlet.PropertyImage); ok {
		prop.Images = v
	}

	go shortletcache.InvalidateMyProperties(context.Background(), userID.String())

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "draft saved — continue editing and publish when ready",
		"property": prop,
		"draft":    draft,
	})
}

// ============================================================================
// PATCH /properties/{id}/publish  — validate draft and flip to 'active'
// ============================================================================

// PublishProperty godoc
// @Summary      Publish a draft property listing
// @Description  Validates that the draft has all required fields and sets status to 'active', making it publicly searchable. Returns a 400 with a list of missing fields if validation fails so the client can prompt the owner to complete them.
// @Tags         Properties
// @Produce      json
// @Param        id  path  string  true  "Property UUID"
// @Success      200  {object}  PropertyResponse
// @Failure      400  {object}  object{error=string,missing_fields=[]string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id}/publish [patch]
// @Security     BearerAuth
func PublishProperty(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only property owners can publish listings", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var prop shortlet.Property
	var imagesRaw, amenitiesRaw, rulesRaw []byte
	var currentStatus string

	err = db.QueryRow(ctx, `
		SELECT id, owner_id, name, description, property_type, status,
		       price_per_night, caution_fee,
		       images, amenities, house_rules,
		       max_adults, max_children,
		       state, city, street,
		       ST_Y(location::geometry) AS latitude,
		       ST_X(location::geometry) AS longitude,
		       avg_rating, review_count, created_at, updated_at
		FROM properties
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL
	`, propID, userID).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &currentStatus,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	if currentStatus == "draft" {
		var draftRaw []byte
		db.QueryRow(ctx, `SELECT draft_data FROM properties WHERE id = $1`, propID).Scan(&draftRaw)

		if len(draftRaw) > 0 {
			var draft map[string]interface{}
			if json.Unmarshal(draftRaw, &draft) == nil {
				if v, ok := draft["name"].(string); ok && v != "" {
					prop.Name = v
				}
				if v, ok := draft["description"].(string); ok {
					prop.Description = &v
				}
				if v, ok := draft["property_type"].(string); ok && v != "" {
					prop.PropertyType = v
				}
				if v, ok := draft["price_per_night"].(float64); ok {
					prop.PricePerNight = v
				}
				if v, ok := draft["caution_fee"].(float64); ok {
					prop.CautionFee = v
				}
				if v, ok := draft["max_adults"].(float64); ok {
					prop.MaxAdults = int(v)
				}
				if v, ok := draft["max_children"].(float64); ok {
					prop.MaxChildren = int(v)
				}
				if v, ok := draft["state"].(string); ok && v != "" {
					prop.State = v
				}
				if v, ok := draft["city"].(string); ok && v != "" {
					prop.City = v
				}
				if v, ok := draft["street"].(string); ok && v != "" {
					prop.Street = v
				}
				if v, ok := draft["amenities"].([]interface{}); ok {
					amenities := make([]string, 0, len(v))
					for _, a := range v {
						if s, ok := a.(string); ok {
							amenities = append(amenities, s)
						}
					}
					prop.Amenities = amenities
				}
				if v, ok := draft["house_rules"].([]interface{}); ok {
					rules := make([]string, 0, len(v))
					for _, r := range v {
						if s, ok := r.(string); ok {
							rules = append(rules, s)
						}
					}
					prop.HouseRules = rules
				}
				if v, ok := draft["images"].([]interface{}); ok && len(v) > 0 {
					imagesJSON, _ := json.Marshal(v)
					var images []shortlet.PropertyImage
					if json.Unmarshal(imagesJSON, &images) == nil {
						prop.Images = images
					}
				}
			}
		}
	}

	validTypes := map[string]bool{
		"apartment": true, "studio": true, "1_bedroom": true, "2_bedroom": true,
		"3_bedroom": true, "4_bedroom": true, "5_bedroom_plus": true,
		"duplex": true, "penthouse": true, "villa": true, "bungalow": true,
	}

	var missing []string
	if strings.TrimSpace(prop.Name) == "" || prop.Name == "Untitled Draft" {
		missing = append(missing, "name")
	}
	if !validTypes[prop.PropertyType] {
		missing = append(missing, "property_type")
	}
	if prop.PricePerNight <= 0 {
		missing = append(missing, "price_per_night")
	}
	if prop.MaxAdults < 1 {
		missing = append(missing, "max_adults")
	}
	if strings.TrimSpace(prop.State) == "" || prop.State == "—" {
		missing = append(missing, "state")
	}
	if strings.TrimSpace(prop.City) == "" || prop.City == "—" {
		missing = append(missing, "city")
	}
	if strings.TrimSpace(prop.Street) == "" || prop.Street == "—" {
		missing = append(missing, "street")
	}
	if len(prop.Images) == 0 {
		missing = append(missing, "images (at least 1 required)")
	}

	if len(missing) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "listing is incomplete — please fill in the missing fields before publishing",
			"missing_fields": missing,
		})
		return
	}

	imagesPublishJSON, _ := json.Marshal(prop.Images)
	amenitiesPublishJSON, _ := json.Marshal(prop.Amenities)
	rulesPublishJSON, _ := json.Marshal(prop.HouseRules)

	err = db.QueryRow(ctx, `
		UPDATE properties
		SET status          = 'active',
			draft_data      = NULL,
			name            = $3,
			description     = $4,
			property_type   = $5::property_type,
			price_per_night = $6,
			caution_fee     = $7,
			images          = $8::jsonb,
			amenities       = $9::jsonb,
			house_rules     = $10::jsonb,
			max_adults      = $11,
			max_children    = $12,
			state           = $13,
			city            = $14,
			street          = $15,
			updated_at      = NOW()
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL
		RETURNING id, owner_id, name, description, property_type, status,
				price_per_night, caution_fee,
				images, amenities, house_rules,
				max_adults, max_children,
				state, city, street,
				ST_Y(location::geometry) AS latitude,
				ST_X(location::geometry) AS longitude,
				avg_rating, review_count, created_at, updated_at
	`,
		propID, userID,
		prop.Name, prop.Description, prop.PropertyType,
		prop.PricePerNight, prop.CautionFee,
		imagesPublishJSON, amenitiesPublishJSON, rulesPublishJSON,
		prop.MaxAdults, prop.MaxChildren,
		prop.State, prop.City, prop.Street,
	).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &prop.Status,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to publish property %s: %v", propID, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	go func() {
		bgCtx := context.Background()
		shortletcache.InvalidateProperty(bgCtx, propID.String())
		shortletcache.InvalidateMyProperties(bgCtx, userID.String())
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "property published successfully",
		"property": prop,
	})
}

// ============================================================================
// GET /owners/me/properties/drafts
// ============================================================================

// GetMyDraftProperties godoc
// @Summary      Get owner's draft listings
// @Description  Returns all properties with status='draft' for the authenticated owner, including the partial draft_data payload so the client can pre-fill the form.
// @Tags         Properties
// @Produce      json
// @Param        page   query  integer false  "Page (default 1)"
// @Param        limit  query  integer false  "Items per page (default 20)"
// @Success 200 {object} PropertyListResponse
// @Failure      403  {object}  object{error=string}
// @Router       /owners/me/properties/drafts [get]
// @Security     BearerAuth
func GetMyDraftProperties(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only owners can access this endpoint", http.StatusForbidden)
		return
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var total int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM properties
		WHERE owner_id = $1 AND status = 'draft' AND deleted_at IS NULL
	`, userID).Scan(&total)

	rows, err := db.Query(ctx, `
		SELECT id, owner_id, name, description, property_type, status,
		       price_per_night, caution_fee,
		       images, amenities, house_rules,
		       max_adults, max_children,
		       state, city, street,
		       ST_Y(location::geometry), ST_X(location::geometry),
		       avg_rating, review_count,
		       draft_data,
		       created_at, updated_at
		FROM properties
		WHERE owner_id = $1 AND status = 'draft' AND deleted_at IS NULL
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type DraftProperty struct {
		shortlet.Property
		DraftData json.RawMessage `json:"draft_data,omitempty"`
	}

	properties := make([]DraftProperty, 0)
	for rows.Next() {
		var p DraftProperty
		var ir, ar, rr []byte
		var draftRaw []byte
		rows.Scan(
			&p.ID, &p.OwnerID, &p.Name, &p.Description,
			&p.PropertyType, &p.Status,
			&p.PricePerNight, &p.CautionFee,
			&ir, &ar, &rr,
			&p.MaxAdults, &p.MaxChildren,
			&p.State, &p.City, &p.Street,
			&p.Latitude, &p.Longitude,
			&p.AvgRating, &p.ReviewCount,
			&draftRaw,
			&p.CreatedAt, &p.UpdatedAt,
		)
		json.Unmarshal(ir, &p.Images)
		json.Unmarshal(ar, &p.Amenities)
		json.Unmarshal(rr, &p.HouseRules)
		if len(draftRaw) > 0 {
			p.DraftData = json.RawMessage(draftRaw)
		}
		properties = append(properties, p)
	}

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(properties),
		"data":   properties,
		"pagination": map[string]int{
			"total": total, "page": page,
			"limit": limit, "total_pages": totalPages,
		},
	})
}

// ============================================================================
// POST /properties
// ============================================================================

// CreateProperty godoc
// @Summary      Create a property listing
// @Description  Allows an authenticated owner to create a new shortlet property listing. Images are uploaded via multipart form. Up to 5 images are accepted. The listing starts as 'active' by default.
// @Tags         Properties
// @Accept       mpfd
// @Produce      json
// @Param        name           formData  string  true   "Property name"
// @Param        description    formData  string  false  "Property description"
// @Param        property_type  formData  string  true   "One of: apartment, studio, 1_bedroom, 2_bedroom, 3_bedroom, 4_bedroom, 5_bedroom_plus, duplex, penthouse, villa, bungalow"
// @Param        price_per_night formData number true  "Nightly rate in NGN"
// @Param        caution_fee    formData  number  false  "Refundable caution fee in NGN (default 0)"
// @Param        amenities      formData  string  false  "JSON array string e.g. [\"WiFi\",\"AC\",\"Kitchen\"]"
// @Param        house_rules    formData  string  false  "JSON array string e.g. [\"No smoking\",\"No parties\"]"
// @Param        max_adults     formData  integer true   "Max number of adult guests"
// @Param        max_children   formData  integer false  "Max number of children (default 0)"
// @Param        state          formData  string  true   "Nigerian state"
// @Param        city           formData  string  true   "City"
// @Param        street         formData  string  true   "Street address"
// @Param        latitude       formData  number  false  "Latitude coordinate"
// @Param        longitude      formData  number  false  "Longitude coordinate"
// @Param        images         formData  file    false  "Property images (max 5)"
// @Success 201 {object} PropertyResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /properties [post]
// @Security     BearerAuth
func CreateProperty(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only property owners can create listings", http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		utils.WriteError(w, "name is required", http.StatusBadRequest)
		return
	}

	propType := strings.TrimSpace(r.FormValue("property_type"))
	validTypes := map[string]bool{
		"apartment": true, "studio": true, "1_bedroom": true, "2_bedroom": true,
		"3_bedroom": true, "4_bedroom": true, "5_bedroom_plus": true,
		"duplex": true, "penthouse": true, "villa": true, "bungalow": true,
	}
	if !validTypes[propType] {
		utils.WriteError(w, "invalid property_type", http.StatusBadRequest)
		return
	}

	priceStr := r.FormValue("price_per_night")
	if priceStr == "" {
		utils.WriteError(w, "price_per_night is required", http.StatusBadRequest)
		return
	}
	var pricePerNight float64
	if _, err := fmt.Sscanf(priceStr, "%f", &pricePerNight); err != nil || pricePerNight <= 0 {
		utils.WriteError(w, "price_per_night must be a positive number", http.StatusBadRequest)
		return
	}

	var cautionFee float64
	if v := r.FormValue("caution_fee"); v != "" {
		fmt.Sscanf(v, "%f", &cautionFee)
	}

	state := strings.TrimSpace(r.FormValue("state"))
	city := strings.TrimSpace(r.FormValue("city"))
	street := strings.TrimSpace(r.FormValue("street"))
	if state == "" || city == "" || street == "" {
		utils.WriteError(w, "state, city, and street are required", http.StatusBadRequest)
		return
	}

	var maxAdults int = 1
	fmt.Sscanf(r.FormValue("max_adults"), "%d", &maxAdults)
	if maxAdults < 1 {
		utils.WriteError(w, "max_adults must be at least 1", http.StatusBadRequest)
		return
	}
	var maxChildren int
	fmt.Sscanf(r.FormValue("max_children"), "%d", &maxChildren)

	description := r.FormValue("description")

	amenitiesJSON := r.FormValue("amenities")
	var amenities []string
	if amenitiesJSON != "" {
		if err := json.Unmarshal([]byte(amenitiesJSON), &amenities); err != nil {
			utils.WriteError(w, "amenities must be a valid JSON array of strings", http.StatusBadRequest)
			return
		}
	}

	rulesJSON := r.FormValue("house_rules")
	var houseRules []string
	if rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &houseRules); err != nil {
			utils.WriteError(w, "house_rules must be a valid JSON array of strings", http.StatusBadRequest)
			return
		}
	}

	var lat, lng *float64
	if latStr := r.FormValue("latitude"); latStr != "" {
		var v float64
		if _, err := fmt.Sscanf(latStr, "%f", &v); err == nil {
			lat = &v
		}
	}
	if lngStr := r.FormValue("longitude"); lngStr != "" {
		var v float64
		if _, err := fmt.Sscanf(lngStr, "%f", &v); err == nil {
			lng = &v
		}
	}

	var uploadedImages []shortlet.PropertyImage
	if r.MultipartForm != nil && len(r.MultipartForm.File["images"]) > 0 {
		imageHeaders := r.MultipartForm.File["images"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 images allowed", http.StatusBadRequest)
			return
		}
		ctx60, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize image service", http.StatusInternalServerError)
			return
		}
		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()
		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx60, cloud, files, "properties")
		if err != nil {
			utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
			return
		}
		now := time.Now()
		for i, url := range urls {
			uploadedImages = append(uploadedImages, shortlet.PropertyImage{
				URL: url, PublicID: publicIDs[i], UpdatedAt: now,
			})
		}
	}

	imagesJSON, _ := json.Marshal(uploadedImages)
	amenitiesDBJSON, _ := json.Marshal(amenities)
	rulesDBJSON, _ := json.Marshal(houseRules)

	var locationExpr string
	var locationArgs []interface{}
	if lat != nil && lng != nil {
		locationExpr = fmt.Sprintf("ST_SetSRID(ST_MakePoint($%d, $%d), 4326)", 15, 16)
		locationArgs = append(locationArgs, *lng, *lat)
	} else {
		locationExpr = "NULL"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	descArg := interface{}(nil)
	if description != "" {
		descArg = description
	}

	var prop shortlet.Property
	args := []interface{}{
		userID, name, descArg, propType,
		pricePerNight, cautionFee,
		imagesJSON, amenitiesDBJSON, rulesDBJSON,
		maxAdults, maxChildren,
		state, city, street,
	}
	args = append(args, locationArgs...)

	query := fmt.Sprintf(`
		INSERT INTO properties (
			owner_id, name, description, property_type,
			price_per_night, caution_fee,
			images, amenities, house_rules,
			max_adults, max_children,
			state, city, street, location
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			$7, $8, $9,
			$10, $11,
			$12, $13, $14, %s
		)
		RETURNING id, owner_id, name, description, property_type, status,
		          price_per_night, caution_fee,
		          images, amenities, house_rules,
		          max_adults, max_children,
		          state, city, street,
		          ST_Y(location::geometry) AS latitude,
		          ST_X(location::geometry) AS longitude,
		          avg_rating, review_count,
		          created_at, updated_at
	`, locationExpr)

	var imagesRaw, amenitiesRaw, rulesRaw []byte
	err := db.QueryRow(ctx, query, args...).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &prop.Status,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to create property: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	go func() {
		bgCtx := context.Background()
		shortletcache.InvalidateListings(bgCtx)
		shortletcache.InvalidateMyProperties(bgCtx, userID.String())
	}()

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "property listing created successfully",
		"property": prop,
	})
}

// ============================================================================
// PATCH /properties/{id}
// ============================================================================

// UpdateProperty godoc
// @Summary      Update a property listing
// @Description  Allows the owner to update any field of their property listing. Send only the fields you want to change. Images that are not re-uploaded remain unchanged. Sending new images replaces all existing ones (max 5 total).
// @Tags         Properties
// @Accept       mpfd
// @Produce      json
// @Param        id             path      string  true   "Property UUID"
// @Param        name           formData  string  false  "Property name"
// @Param        description    formData  string  false  "Property description"
// @Param        property_type  formData  string  false  "Property type"
// @Param        price_per_night formData number false  "Nightly rate in NGN"
// @Param        caution_fee    formData  number  false  "Caution fee in NGN"
// @Param        amenities      formData  string  false  "JSON array of amenity strings"
// @Param        house_rules    formData  string  false  "JSON array of rule strings"
// @Param        max_adults     formData  integer false  "Max adults"
// @Param        max_children   formData  integer false  "Max children"
// @Param        state          formData  string  false  "State"
// @Param        city           formData  string  false  "City"
// @Param        street         formData  string  false  "Street"
// @Param        latitude       formData  number  false  "Latitude"
// @Param        longitude      formData  number  false  "Longitude"
// @Param        status         formData  string  false  "active or inactive"
// @Param        images         formData  file    false  "New images (replaces all existing, max 5)"
// @Success 200 {object} PropertyResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id} [patch]
// @Security     BearerAuth
func UpdateProperty(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only property owners can update listings", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var existingOwnerID uuid.UUID
	var existingImagesRaw []byte
	var currentStatus string
	err = db.QueryRow(ctx,
		`SELECT owner_id, images, status FROM properties WHERE id = $1 AND deleted_at IS NULL`,
		propID,
	).Scan(&existingOwnerID, &existingImagesRaw, &currentStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "property not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if existingOwnerID != userID {
		utils.WriteError(w, "you do not own this property", http.StatusForbidden)
		return
	}

	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argIdx := 1

	addSet := func(col string, val interface{}) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	isDraft := currentStatus == "draft"
	draftPatch := map[string]interface{}{}

	if v := strings.TrimSpace(r.FormValue("name")); v != "" {
		addSet("name", v)
		if isDraft {
			draftPatch["name"] = v
		}
	}
	if v := r.FormValue("description"); v != "" {
		addSet("description", v)
		if isDraft {
			draftPatch["description"] = v
		}
	}
	if v := r.FormValue("property_type"); v != "" {
		validTypes := map[string]bool{
			"apartment": true, "studio": true, "1_bedroom": true, "2_bedroom": true,
			"3_bedroom": true, "4_bedroom": true, "5_bedroom_plus": true,
			"duplex": true, "penthouse": true, "villa": true, "bungalow": true,
		}
		if !validTypes[v] {
			utils.WriteError(w, "invalid property_type", http.StatusBadRequest)
			return
		}
		addSet("property_type", v)
		if isDraft {
			draftPatch["property_type"] = v
		}
	}
	if v := r.FormValue("price_per_night"); v != "" {
		var price float64
		if _, err := fmt.Sscanf(v, "%f", &price); err != nil || price <= 0 {
			utils.WriteError(w, "price_per_night must be a positive number", http.StatusBadRequest)
			return
		}
		addSet("price_per_night", price)
		if isDraft {
			draftPatch["price_per_night"] = price
		}
	}
	if v := r.FormValue("caution_fee"); v != "" {
		var fee float64
		fmt.Sscanf(v, "%f", &fee)
		addSet("caution_fee", fee)
		if isDraft {
			draftPatch["caution_fee"] = fee
		}
	}
	if v := r.FormValue("amenities"); v != "" {
		var amenities []string
		if err := json.Unmarshal([]byte(v), &amenities); err != nil {
			utils.WriteError(w, "amenities must be a valid JSON array", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(amenities)
		addSet("amenities", b)
		if isDraft {
			draftPatch["amenities"] = amenities
		}
	}
	if v := r.FormValue("house_rules"); v != "" {
		var rules []string
		if err := json.Unmarshal([]byte(v), &rules); err != nil {
			utils.WriteError(w, "house_rules must be a valid JSON array", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(rules)
		addSet("house_rules", b)
		if isDraft {
			draftPatch["house_rules"] = rules
		}
	}
	if v := r.FormValue("max_adults"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n < 1 {
			utils.WriteError(w, "max_adults must be at least 1", http.StatusBadRequest)
			return
		}
		addSet("max_adults", n)
		if isDraft {
			draftPatch["max_adults"] = n
		}
	}
	if v := r.FormValue("max_children"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		addSet("max_children", n)
		if isDraft {
			draftPatch["max_children"] = n
		}
	}
	if v := r.FormValue("state"); v != "" {
		addSet("state", v)
		if isDraft {
			draftPatch["state"] = v
		}
	}
	if v := r.FormValue("city"); v != "" {
		addSet("city", v)
		if isDraft {
			draftPatch["city"] = v
		}
	}
	if v := r.FormValue("street"); v != "" {
		addSet("street", v)
		if isDraft {
			draftPatch["street"] = v
		}
	}
	if v := r.FormValue("status"); v != "" {
		if v == "active" || v == "inactive" {
			addSet("status", v)
		} else if v != "draft" {
			utils.WriteError(w, "status must be active or inactive (use /publish to publish a draft)", http.StatusBadRequest)
			return
		}
	}

	if latStr, lngStr := r.FormValue("latitude"), r.FormValue("longitude"); latStr != "" && lngStr != "" {
		var lat, lng float64
		fmt.Sscanf(latStr, "%f", &lat)
		fmt.Sscanf(lngStr, "%f", &lng)
		setClauses = append(setClauses, fmt.Sprintf("location = ST_SetSRID(ST_MakePoint($%d, $%d), 4326)", argIdx, argIdx+1))
		args = append(args, lng, lat)
		argIdx += 2
		if isDraft {
			draftPatch["latitude"] = lat
			draftPatch["longitude"] = lng
		}
	}

	if isDraft && len(draftPatch) > 0 {
		// Merge new fields into draft_data using jsonb concat operator
		patchJSON, _ := json.Marshal(draftPatch)
		setClauses = append(setClauses, fmt.Sprintf(
			"draft_data = COALESCE(draft_data, '{}'::jsonb) || $%d::jsonb", argIdx,
		))
		args = append(args, string(patchJSON))
		argIdx++
	}

	if r.MultipartForm != nil && len(r.MultipartForm.File["images"]) > 0 {
		imageHeaders := r.MultipartForm.File["images"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 images allowed", http.StatusBadRequest)
			return
		}
		ctx60, cancel60 := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel60()
		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize image service", http.StatusInternalServerError)
			return
		}
		var oldImages []shortlet.PropertyImage
		if json.Unmarshal(existingImagesRaw, &oldImages) == nil {
			var oldIDs []string
			for _, img := range oldImages {
				if img.PublicID != "" {
					oldIDs = append(oldIDs, img.PublicID)
				}
			}
			if len(oldIDs) > 0 {
				go handlers.CleanupUploads(context.Background(), cloud, oldIDs)
			}
		}
		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()
		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx60, cloud, files, "properties")
		if err != nil {
			utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
			return
		}
		now := time.Now()
		var newImages []shortlet.PropertyImage
		for i, url := range urls {
			newImages = append(newImages, shortlet.PropertyImage{
				URL: url, PublicID: publicIDs[i], UpdatedAt: now,
			})
		}
		b, _ := json.Marshal(newImages)
		addSet("images", b)
	}

	if len(setClauses) == 1 {
		utils.WriteError(w, "no fields provided to update", http.StatusBadRequest)
		return
	}

	args = append(args, propID)
	query := fmt.Sprintf(`
		UPDATE properties SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, owner_id, name, description, property_type, status,
		          price_per_night, caution_fee,
		          images, amenities, house_rules,
		          max_adults, max_children,
		          state, city, street,
		          ST_Y(location::geometry) AS latitude,
		          ST_X(location::geometry) AS longitude,
		          avg_rating, review_count, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIdx)

	var prop shortlet.Property
	var imagesRaw, amenitiesRaw, rulesRaw []byte
	err = db.QueryRow(ctx, query, args...).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &prop.Status,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to update property: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	go func() {
		bgCtx := context.Background()
		shortletcache.InvalidateProperty(bgCtx, propID.String())
		shortletcache.InvalidateMyProperties(bgCtx, userID.String())
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "property updated successfully",
		"property": prop,
	})
}

// ============================================================================
// DELETE /properties/{id}
// ============================================================================

// DeleteProperty godoc
// @Summary      Delete a property listing
// @Description  Soft-deletes a property. Only the owner can delete it. Properties with active confirmed orders cannot be deleted.
// @Tags         Properties
// @Produce      json
// @Param        id  path  string  true  "Property UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /properties/{id} [delete]
// @Security     BearerAuth
func DeleteProperty(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only property owners can delete listings", http.StatusForbidden)
		return
	}

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var activeCount int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM orders
		WHERE property_id = $1 AND status IN ('confirmed','checked_in')
	`, propID).Scan(&activeCount)
	if activeCount > 0 {
		utils.WriteError(w, fmt.Sprintf("cannot delete — property has %d active order(s)", activeCount), http.StatusConflict)
		return
	}

	result, err := db.Exec(ctx, `
		UPDATE properties SET deleted_at = NOW(), status = 'inactive', updated_at = NOW()
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL
	`, propID, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "property not found or you do not own it", http.StatusNotFound)
		return
	}

	go func() {
		bgCtx := context.Background()
		shortletcache.InvalidateProperty(bgCtx, propID.String())
		shortletcache.InvalidateMyProperties(bgCtx, userID.String())
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "property listing deleted",
	})
}

// ============================================================================
// GET /properties/{id}
// ============================================================================

// GetProperty godoc
// @Summary      Get a single property
// @Description  Returns full details of a property including its review summary and owner profile (name, avatar, bio). Clients can access active listings; owners can access their own regardless of status.
// @Tags         Properties
// @Produce      json
// @Param        id  path  string  true  "Property UUID"
// @Success      200  {object}  PropertyDetailResponse
// @Failure      404  {object}  object{error=string}
// @Router       /properties/{id} [get]
// @Security     BearerAuth
func GetProperty(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, _ := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)

	propID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid property id", http.StatusBadRequest)
		return
	}

	cacheKey := shortletcache.KeyPropertyDetail(propID.String())
	if role != "owner" {
		type cachedResp struct {
			Status  string                 `json:"status"`
			Data    shortlet.Property      `json:"data"`
			IsSaved bool                   `json:"is_saved"`
			Owner   map[string]interface{} `json:"owner"`
		}
		var cached cachedResp
		if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
			if role == "client" && userID != uuid.Nil {
				var isSaved bool
				ctx2, cancel2 := context.WithTimeout(r.Context(), 3*time.Second)
				defer cancel2()
				db.QueryRow(ctx2, `
					SELECT EXISTS(SELECT 1 FROM saved_listings WHERE client_id = $1 AND property_id = $2)
				`, userID, propID).Scan(&isSaved)
				cached.IsSaved = isSaved
			}
			utils.WriteJSON(w, cached)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query := `
		SELECT p.id, p.owner_id, p.name, p.description, p.property_type, p.status,
		       p.price_per_night, p.caution_fee,
		       p.images, p.amenities, p.house_rules,
		       p.max_adults, p.max_children,
		       p.state, p.city, p.street,
		       ST_Y(p.location::geometry) AS latitude,
		       ST_X(p.location::geometry) AS longitude,
		       p.avg_rating, p.review_count,
		       p.created_at, p.updated_at,
		       u.first_name || ' ' || u.last_name AS owner_name,
		       u.avatar AS owner_avatar,
		       u.bio AS owner_bio
		FROM properties p
		JOIN users u ON u.id = p.owner_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`

	if role != "owner" {
		query += " AND p.status = 'active'"
	} else {
		query += " AND (p.status = 'active' OR p.status = 'draft' OR p.owner_id = $2)"
	}

	var prop shortlet.Property
	var imagesRaw, amenitiesRaw, rulesRaw, ownerAvatarRaw []byte
	var ownerName string
	var ownerBio *string

	args := []interface{}{propID}
	if role == "owner" {
		args = append(args, userID)
	}

	err = db.QueryRow(ctx, query, args...).Scan(
		&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
		&prop.PropertyType, &prop.Status,
		&prop.PricePerNight, &prop.CautionFee,
		&imagesRaw, &amenitiesRaw, &rulesRaw,
		&prop.MaxAdults, &prop.MaxChildren,
		&prop.State, &prop.City, &prop.Street,
		&prop.Latitude, &prop.Longitude,
		&prop.AvgRating, &prop.ReviewCount,
		&prop.CreatedAt, &prop.UpdatedAt,
		&ownerName, &ownerAvatarRaw,
		&ownerBio,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "property not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch property: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.Unmarshal(imagesRaw, &prop.Images)
	json.Unmarshal(amenitiesRaw, &prop.Amenities)
	json.Unmarshal(rulesRaw, &prop.HouseRules)

	var isSaved bool
	if role == "client" && userID != uuid.Nil {
		db.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM saved_listings WHERE client_id = $1 AND property_id = $2)
		`, userID, propID).Scan(&isSaved)
	}

	var ownerAvatarURL *string
	if len(ownerAvatarRaw) > 0 {
		var av struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(ownerAvatarRaw, &av) == nil && av.URL != "" {
			ownerAvatarURL = &av.URL
		}
	}

	ownerMap := map[string]interface{}{
		"id":     prop.OwnerID,
		"name":   ownerName,
		"avatar": ownerAvatarURL,
		"bio":    ownerBio,
	}

	resp := map[string]interface{}{
		"status":   "success",
		"data":     prop,
		"is_saved": isSaved,
		"owner":    ownerMap,
	}

	if role != "owner" {
		go shortletcache.SetCached(
			context.Background(),
			cacheKey,
			map[string]interface{}{
				"status":   "success",
				"data":     prop,
				"is_saved": false,
				"owner":    ownerMap,
			},
			shortletcache.TTLPropertyDetail,
		)
	}

	utils.WriteJSON(w, resp)
}

// ============================================================================
// GET /properties  (public listing with advanced filters)
// ============================================================================

// ListProperties godoc
// @Summary      List / search properties
// @Description  Returns a paginated list of active properties. Supports rich filtering, full-text search, and sorting.
// @Tags         Properties
// @Produce      json
// @Param        state           query  string  false  "Filter by state"
// @Param        city            query  string  false  "Filter by city"
// @Param        property_type   query  string  false  "Filter by type: apartment, studio, 1_bedroom, etc."
// @Param        min_price       query  number  false  "Minimum price per night"
// @Param        max_price       query  number  false  "Maximum price per night"
// @Param        min_rating      query  number  false  "Minimum average rating (0–5)"
// @Param        max_rating      query  number  false  "Maximum average rating"
// @Param        min_reviews     query  integer false  "Minimum review count"
// @Param        amenities       query  string  false  "Comma-separated list of required amenities e.g. WiFi,AC"
// @Param        min_adults      query  integer false  "Minimum adult capacity"
// @Param        min_children    query  integer false  "Minimum children capacity"
// @Param        checkin         query  string  false  "Check-in date YYYY-MM-DD — filters to available properties"
// @Param        checkout        query  string  false  "Check-out date YYYY-MM-DD — filters to available properties"
// @Param        search          query  string  false  "Full-text search on name, description, city, street"
// @Param        sort            query  string  false  "Sort field: price_asc, price_desc, rating_desc, newest (default: newest)"
// @Param        page            query  integer false  "Page number (default 1)"
// @Param        limit           query  integer false  "Items per page (default 20, max 50)"
// @Success 200 {object} PropertyListResponse
// @Router       /properties [get]
func ListProperties(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	cacheKey := shortletcache.KeyPropertyList(r.URL.RawQuery)
	type listResp struct {
		Status     string              `json:"status"`
		Count      int                 `json:"count"`
		Data       []shortlet.Property `json:"data"`
		Pagination map[string]int      `json:"pagination"`
	}
	var cached listResp
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
		utils.WriteJSON(w, cached)
		return
	}

	q := r.URL.Query()

	page, limit := utils.GetPaginationParams(r)
	if limit > 50 {
		limit = 50
	}
	offset := (page - 1) * limit

	args := []interface{}{}
	argIdx := 1
	conditions := []string{"p.deleted_at IS NULL", "p.status = 'active'"}

	addCond := func(cond string, val interface{}) {
		conditions = append(conditions, fmt.Sprintf(cond, argIdx))
		args = append(args, val)
		argIdx++
	}

	if v := q.Get("state"); v != "" {
		addCond("p.state ILIKE $%d", "%"+v+"%")
	}
	if v := q.Get("city"); v != "" {
		addCond("p.city ILIKE $%d", "%"+v+"%")
	}
	if v := q.Get("property_type"); v != "" {
		addCond("p.property_type = $%d", v)
	}
	if v := q.Get("min_price"); v != "" {
		var n float64
		fmt.Sscanf(v, "%f", &n)
		addCond("p.price_per_night >= $%d", n)
	}
	if v := q.Get("max_price"); v != "" {
		var n float64
		fmt.Sscanf(v, "%f", &n)
		addCond("p.price_per_night <= $%d", n)
	}
	if v := q.Get("min_rating"); v != "" {
		var n float64
		fmt.Sscanf(v, "%f", &n)
		addCond("p.avg_rating >= $%d", n)
	}
	if v := q.Get("max_rating"); v != "" {
		var n float64
		fmt.Sscanf(v, "%f", &n)
		addCond("p.avg_rating <= $%d", n)
	}
	if v := q.Get("min_reviews"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		addCond("p.review_count >= $%d", n)
	}
	if v := q.Get("min_adults"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		addCond("p.max_adults >= $%d", n)
	}
	if v := q.Get("min_children"); v != "" {
		var n int
		fmt.Sscanf(v, "%d", &n)
		addCond("p.max_children >= $%d", n)
	}

	if v := q.Get("amenities"); v != "" {
		parts := strings.Split(v, ",")
		for _, a := range parts {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			conditions = append(conditions, fmt.Sprintf("p.amenities @> $%d::jsonb", argIdx))
			b, _ := json.Marshal([]string{a})
			args = append(args, string(b))
			argIdx++
		}
	}

	if v := q.Get("search"); v != "" {
		search := "%" + strings.TrimSpace(v) + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(p.name ILIKE $%d OR p.description ILIKE $%d OR p.city ILIKE $%d OR p.street ILIKE $%d)",
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, search)
		argIdx++
	}

	checkin := q.Get("checkin")
	checkout := q.Get("checkout")
	if checkin != "" && checkout != "" {

		conditions = append(conditions, fmt.Sprintf(`
			NOT EXISTS (
				SELECT 1 FROM orders o
				WHERE o.property_id = p.id
				  AND o.status IN ('confirmed', 'checked_in', 'pending')
				  AND o.check_in_date < $%d
				  AND o.check_out_date > $%d
			)
		`, argIdx, argIdx+1))
		args = append(args, checkout, checkin)
		argIdx += 2

		conditions = append(conditions, fmt.Sprintf(`
			NOT EXISTS (
				SELECT 1 FROM property_availability_overrides pao
				WHERE pao.property_id = p.id
				  AND pao.blocked_date >= $%d
				  AND pao.blocked_date < $%d
			)
		`, argIdx, argIdx+1))
		args = append(args, checkin, checkout)
		argIdx += 2
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	orderBy := "ORDER BY p.created_at DESC"
	switch q.Get("sort") {
	case "price_asc":
		orderBy = "ORDER BY p.price_per_night ASC"
	case "price_desc":
		orderBy = "ORDER BY p.price_per_night DESC"
	case "rating_desc":
		orderBy = "ORDER BY p.avg_rating DESC, p.review_count DESC"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var total int
	db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM properties p %s`, where), args...).Scan(&total)

	fetchArgs := append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT p.id, p.owner_id, p.name, p.description, p.property_type, p.status,
		       p.price_per_night, p.caution_fee,
		       p.images, p.amenities, p.house_rules,
		       p.max_adults, p.max_children,
		       p.state, p.city, p.street,
		       ST_Y(p.location::geometry) AS latitude,
		       ST_X(p.location::geometry) AS longitude,
		       p.avg_rating, p.review_count, p.created_at, p.updated_at
		FROM properties p
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, where, orderBy, argIdx, argIdx+1), fetchArgs...)
	if err != nil {
		utils.Logger.Errorf("failed to list properties: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	properties := make([]shortlet.Property, 0)
	for rows.Next() {
		var prop shortlet.Property
		var imagesRaw, amenitiesRaw, rulesRaw []byte
		if err := rows.Scan(
			&prop.ID, &prop.OwnerID, &prop.Name, &prop.Description,
			&prop.PropertyType, &prop.Status,
			&prop.PricePerNight, &prop.CautionFee,
			&imagesRaw, &amenitiesRaw, &rulesRaw,
			&prop.MaxAdults, &prop.MaxChildren,
			&prop.State, &prop.City, &prop.Street,
			&prop.Latitude, &prop.Longitude,
			&prop.AvgRating, &prop.ReviewCount,
			&prop.CreatedAt, &prop.UpdatedAt,
		); err != nil {
			utils.Logger.Errorf("scan error: %v", err)
			continue
		}
		json.Unmarshal(imagesRaw, &prop.Images)
		json.Unmarshal(amenitiesRaw, &prop.Amenities)
		json.Unmarshal(rulesRaw, &prop.HouseRules)
		properties = append(properties, prop)
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

	go shortletcache.SetCached(context.Background(), cacheKey, result, shortletcache.TTLPropertyList)

	utils.WriteJSON(w, result)
}

// ============================================================================
// GET /owners/me/properties  — owner's own listings
// ============================================================================

// GetMyProperties godoc
// @Summary      Get owner's own listings
// @Description  Returns all properties (active, inactive, pending_review) belonging to the authenticated owner.
// @Tags         Properties
// @Produce      json
// @Param        status  query  string  false  "Filter by status: active, inactive, pending_review, suspended"
// @Param        page    query  integer false  "Page (default 1)"
// @Param        limit   query  integer false  "Items per page (default 20)"
// @Success 200 {object} PropertyListResponse
// @Failure      403  {object}  object{error=string}
// @Router       /owners/me/properties [get]
// @Security     BearerAuth
func GetMyProperties(w http.ResponseWriter, r *http.Request) {
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
	if role != "owner" {
		utils.WriteError(w, "only owners can access this endpoint", http.StatusForbidden)
		return
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit
	statusFilter := r.URL.Query().Get("status")

	cacheKey := shortletcache.KeyMyProperties(userID.String(), statusFilter, page, limit)
	type listResp struct {
		Status     string              `json:"status"`
		Count      int                 `json:"count"`
		Data       []shortlet.Property `json:"data"`
		Pagination map[string]int      `json:"pagination"`
	}
	var cached listResp
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cached); hit {
		utils.WriteJSON(w, cached)
		return
	}

	args := []interface{}{userID}
	where := "owner_id = $1 AND deleted_at IS NULL AND status != 'draft'"

	if statusFilter != "" {
		where += " AND status = $2"
		args = append(args, statusFilter)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var total int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM properties WHERE "+where, args...).Scan(&total)

	fetchArgs := append(args, limit, offset)
	argOffset := len(args) + 1
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT id, owner_id, name, description, property_type, status,
		       price_per_night, caution_fee,
		       images, amenities, house_rules,
		       max_adults, max_children,
		       state, city, street,
		       ST_Y(location::geometry), ST_X(location::geometry),
		       avg_rating, review_count, created_at, updated_at
		FROM properties
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argOffset, argOffset+1), fetchArgs...)
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

	go shortletcache.SetCached(context.Background(), cacheKey, result, shortletcache.TTLMyProperties)

	utils.WriteJSON(w, result)
}
