package artisanprofilesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// Models
// ============================================================================

// CategoryInfo is used to populate category details in responses
type CategoryInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ArtisanCategory struct {
	ID        uuid.UUID    `json:"id"`
	ArtisanID uuid.UUID    `json:"artisan_id"`
	Category  CategoryInfo `json:"category"`
	CreatedAt time.Time    `json:"created_at"`
}

type PortfolioImage struct {
	ID        uuid.UUID    `json:"id"`
	ArtisanID uuid.UUID    `json:"artisan_id"`
	Category  CategoryInfo `json:"category"` // populated
	ImageURL  string       `json:"image_url"`
	PublicID  string       `json:"public_id"`
	Caption   *string      `json:"caption,omitempty"`
	SortOrder int          `json:"sort_order"`
	CreatedAt time.Time    `json:"created_at"`
}

// ============================================================================
// GET /artisan/categories  — list all job categories (public)
// ============================================================================

// GetAllCategories godoc
// @Summary      List all job categories
// @Description  Returns all platform job categories. Used by clients and artisans to browse available service types.
// @Tags         Categories
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]CategoryInfo}
// @Router       /artisan/categories [get]
func GetAllCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT id, name FROM job_categories ORDER BY name ASC
	`)
	if err != nil {
		utils.Logger.Errorf("failed to fetch categories: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cats := make([]CategoryInfo, 0)
	for rows.Next() {
		var c CategoryInfo
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		cats = append(cats, c)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(cats),
		"data":   cats,
	})
}

// ============================================================================
// POST /artisan/categories
// ============================================================================
// category_id is an INT (job_categories.id is SERIAL, not UUID).

// AddArtisanCategory godoc
// @Summary      Add a service category
// @Description  Registers a job category on the artisan's profile. An artisan can have at most 2 categories. The DB enforces this with a trigger.
// @Tags         Artisan Profile
// @Accept       json
// @Produce      json
// @Param        body  body  object{category_id=int}  true  "Category ID (integer)"
// @Success      201   {object}  object{status=string,message=string,data=ArtisanCategory}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /artisan/categories [post]
// @Security     BearerAuth
func AddArtisanCategory(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can add service categories", http.StatusForbidden)
		return
	}

	type request struct {
		CategoryID uuid.UUID `json:"category_id"`
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var cat CategoryInfo
	err := db.QueryRow(ctx, `
		SELECT id, name FROM job_categories WHERE id = $1
	`, req.CategoryID).Scan(&cat.ID, &cat.Name)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "category not found", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("failed to fetch category: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var ac ArtisanCategory
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_categories (artisan_id, category_id)
		VALUES ($1, $2)
		RETURNING id, artisan_id, created_at
	`, userID, req.CategoryID).Scan(&ac.ID, &ac.ArtisanID, &ac.CreatedAt)
	if err != nil {
		if isPgUniqueViolation(err) {
			utils.WriteError(w, "you have already added this category", http.StatusConflict)
			return
		}
		if pgErrCode(err) == "P0001" {
			utils.WriteError(w, "you can only register up to 2 service categories — remove one before adding another", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to insert artisan_category: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ac.Category = cat

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "category added to your profile",
		"data":    ac,
	})
}

// ============================================================================
// GET /artisan/categories
// ============================================================================

// GetMyCategories godoc
// @Summary      Get own registered categories
// @Description  Returns the categories registered on the authenticated artisan's profile, with category details populated.
// @Tags         Artisan Profile
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]ArtisanCategory}
// @Router       /artisan/categories [get]
// @Security     BearerAuth
func GetMyCategories(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(r.Context(), `
		SELECT ac.id, ac.artisan_id, jc.id, jc.name, ac.created_at
		FROM artisan_categories ac
		JOIN job_categories jc ON jc.id = ac.category_id
		WHERE ac.artisan_id = $1
		ORDER BY ac.created_at ASC
	`, userID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch artisan categories: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cats := make([]ArtisanCategory, 0)
	for rows.Next() {
		var ac ArtisanCategory
		if err := rows.Scan(
			&ac.ID, &ac.ArtisanID,
			&ac.Category.ID, &ac.Category.Name,
			&ac.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		cats = append(cats, ac)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(cats),
		"data":   cats,
	})
}

// ============================================================================
// GET /artisan/{id}/categories
// ============================================================================

// GetArtisanCategories godoc
// @Summary      Get an artisan's categories (public)
// @Description  Returns the categories registered on a specific artisan's profile, with category details populated.
// @Tags         Artisan Profile
// @Produce      json
// @Param        id  path  string  true  "Artisan UUID"
// @Success      200  {object}  object{status=string,count=int,data=[]ArtisanCategory}
// @Failure      400  {object}  object{error=string}
// @Router       /artisan/{id}/categories [get]
func GetArtisanCategories(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(r.Context(), `
		SELECT ac.id, ac.artisan_id, jc.id, jc.name, ac.created_at
		FROM artisan_categories ac
		JOIN job_categories jc ON jc.id = ac.category_id
		WHERE ac.artisan_id = $1
		ORDER BY ac.created_at ASC
	`, artisanID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch artisan categories: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	cats := make([]ArtisanCategory, 0)
	for rows.Next() {
		var ac ArtisanCategory
		if err := rows.Scan(
			&ac.ID, &ac.ArtisanID,
			&ac.Category.ID, &ac.Category.Name,
			&ac.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		cats = append(cats, ac)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(cats),
		"data":   cats,
	})
}

// ============================================================================
// DELETE /artisan/categories/{categoryId}
// ============================================================================
// categoryId here is the INT id of job_categories, not the UUID of artisan_categories.

// RemoveArtisanCategory godoc
// @Summary      Remove a service category
// @Description  Removes a category from the artisan's profile. All portfolio images and services tied to that category are cascade-deleted. Will fail if any service under this category has active bookings.
// @Tags         Artisan Profile
// @Produce      json
// @Param        categoryId  path  int  true  "Category ID (integer)"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /artisan/categories/{categoryId} [delete]
// @Security     BearerAuth
func RemoveArtisanCategory(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can remove service categories", http.StatusForbidden)
		return
	}

	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Block if active bookings exist under any service in this category
	var activeBookings int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM artisan_bookings ab
		JOIN artisan_services s ON s.id = ab.service_id
		WHERE s.artisan_id  = $1
		  AND s.category_id = $2
		  AND ab.status IN ('pending', 'confirmed')
	`, userID, categoryID).Scan(&activeBookings)
	if activeBookings > 0 {
		utils.WriteError(w, "cannot remove this category — you have active bookings under it. Cancel them first.", http.StatusConflict)
		return
	}

	// Fetch Cloudinary public IDs to clean up after deletion
	imageRows, _ := db.Query(ctx, `
		SELECT public_id FROM artisan_portfolio_images
		WHERE artisan_id = $1 AND category_id = $2 AND public_id != ''
	`, userID, categoryID)

	var publicIDs []string
	if imageRows != nil {
		defer imageRows.Close()
		for imageRows.Next() {
			var pid string
			if err := imageRows.Scan(&pid); err == nil {
				publicIDs = append(publicIDs, pid)
			}
		}
	}

	result, err := db.Exec(ctx, `
		DELETE FROM artisan_categories
		WHERE artisan_id = $1 AND category_id = $2
	`, userID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to remove artisan category: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "category not found on your profile", http.StatusNotFound)
		return
	}

	// Clean up Cloudinary images asynchronously after successful DB delete
	if len(publicIDs) > 0 {
		go func(pids []string) {
			cloud, err := utils.InitCloudinary()
			if err != nil {
				utils.Logger.Warnf("failed to init cloudinary for portfolio cleanup: %v", err)
				return
			}
			handlers.CleanupUploads(context.Background(), cloud, pids)
		}(publicIDs)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "category removed from your profile",
	})
}

// ============================================================================
// POST /artisan/categories/{categoryId}/portfolio
// ============================================================================
// categoryId is the INT id of job_categories.
// Accepts multipart/form-data with field "images" (multiple files).

// AddPortfolioImages godoc
// @Summary      Upload portfolio images for a category
// @Description  Uploads up to 5 sample-work images for one of the artisan's registered categories. Send as multipart/form-data with field name "images". The DB enforces a per-category limit of 5 images.
// @Tags         Artisan Profile
// @Accept       mpfd
// @Produce      json
// @Param        categoryId  path      int     true  "Category ID (integer)"
// @Param        images      formData  file    true  "Image file(s)"
// @Param        captions    formData  string  false "JSON array of captions e.g. [\"caption1\",\"\"]"
// @Success      201  {object}  object{status=string,message=string,uploaded=int,data=[]PortfolioImage}
// @Failure      400  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /artisan/categories/{categoryId}/portfolio [post]
// @Security     BearerAuth
func AddPortfolioImages(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can upload portfolio images", http.StatusForbidden)
		return
	}

	// categoryId is integer
	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Verify artisan has this category registered
	var catRegistered bool
	_ = db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM artisan_categories
			WHERE artisan_id = $1 AND category_id = $2
		)
	`, userID, categoryID).Scan(&catRegistered)
	if !catRegistered {
		utils.WriteError(w, "this category is not registered on your profile — add it first", http.StatusBadRequest)
		return
	}

	// Fetch category name for response
	var cat CategoryInfo
	_ = db.QueryRow(ctx, `SELECT id, name FROM job_categories WHERE id = $1`, categoryID).Scan(&cat.ID, &cat.Name)

	// Check how many images already exist
	var currentCount int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM artisan_portfolio_images
		WHERE artisan_id = $1 AND category_id = $2
	`, userID, categoryID).Scan(&currentCount)

	const maxImages = 5
	remaining := maxImages - currentCount
	if remaining <= 0 {
		utils.WriteError(w, "you already have 5 portfolio images for this category — delete one before uploading more", http.StatusConflict)
		return
	}

	// Parse multipart form — 25 MB max
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data — make sure you are sending multipart/form-data", http.StatusBadRequest)
		return
	}

	fileHeaders := r.MultipartForm.File["images"]
	if len(fileHeaders) == 0 {
		utils.WriteError(w, "at least one image is required (field name: images)", http.StatusBadRequest)
		return
	}
	if len(fileHeaders) > remaining {
		utils.WriteError(w,
			fmt.Sprintf("too many images — you can upload at most %d more for this category", remaining),
			http.StatusBadRequest,
		)
		return
	}

	// Parse optional captions JSON array
	captions := make([]string, len(fileHeaders))
	if rawCaptions := r.FormValue("captions"); rawCaptions != "" {
		var parsed []string
		if err := json.Unmarshal([]byte(rawCaptions), &parsed); err == nil {
			for i := 0; i < len(captions) && i < len(parsed); i++ {
				captions[i] = parsed[i]
			}
		}
	}

	// Open all files before uploading
	uploadFiles := make([]utils.UploadFile, 0, len(fileHeaders))
	closers := make([]io.Closer, 0, len(fileHeaders))
	for _, fh := range fileHeaders {
		f, err := fh.Open()
		if err != nil {
			// Close any already-opened files
			for _, c := range closers {
				c.Close()
			}
			utils.WriteError(w, "failed to read uploaded file: "+fh.Filename, http.StatusBadRequest)
			return
		}
		closers = append(closers, f)
		uploadFiles = append(uploadFiles, utils.UploadFile{Reader: f, Filename: fh.Filename})
	}
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()

	cloud, err := utils.InitCloudinary()
	if err != nil {
		utils.WriteError(w, "failed to initialize storage", http.StatusInternalServerError)
		return
	}

	urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, uploadFiles, "artisan_portfolio")
	if err != nil || len(urls) == 0 {
		utils.Logger.Errorf("portfolio image upload error: %v", err)
		if len(publicIDs) > 0 {
			handlers.CleanupUploads(ctx, cloud, publicIDs)
		}
		utils.WriteError(w, "failed to upload images: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Insert all uploaded images in a transaction
	tx, err := db.Begin(ctx)
	if err != nil {
		handlers.CleanupUploads(ctx, cloud, publicIDs)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	uploaded := make([]PortfolioImage, 0, len(urls))
	for i, url := range urls {
		var caption *string
		if i < len(captions) && captions[i] != "" {
			c := captions[i]
			caption = &c
		}

		var img PortfolioImage
		err := tx.QueryRow(ctx, `
			INSERT INTO artisan_portfolio_images
				(artisan_id, category_id, image_url, public_id, caption, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, artisan_id, category_id, image_url, public_id, caption, sort_order, created_at
		`, userID, categoryID, url, publicIDs[i], caption, currentCount+i).Scan(
			&img.ID, &img.ArtisanID, &img.Category.ID,
			&img.ImageURL, &img.PublicID, &img.Caption,
			&img.SortOrder, &img.CreatedAt,
		)
		if err != nil {
			tx.Rollback(ctx)
			handlers.CleanupUploads(ctx, cloud, publicIDs)
			if pgErrCode(err) == "P0001" {
				utils.WriteError(w, "portfolio image limit reached (max 5 per category)", http.StatusConflict)
				return
			}
			utils.Logger.Errorf("failed to insert portfolio image: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// Populate category info on the response object
		img.Category = cat
		uploaded = append(uploaded, img)
	}

	if err := tx.Commit(ctx); err != nil {
		handlers.CleanupUploads(ctx, cloud, publicIDs)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  fmt.Sprintf("%d image(s) uploaded successfully", len(uploaded)),
		"uploaded": len(uploaded),
		"data":     uploaded,
	})
}

// ============================================================================
// GET /artisan/categories/{categoryId}/portfolio
// ============================================================================

// GetMyPortfolioImages godoc
// @Summary      Get own portfolio images for a category
// @Description  Returns all portfolio images the authenticated artisan has uploaded for a given category.
// @Tags         Artisan Profile
// @Produce      json
// @Param        categoryId  path  int  true  "Category ID (integer)"
// @Success      200  {object}  object{status=string,count=int,data=[]PortfolioImage}
// @Router       /artisan/categories/{categoryId}/portfolio [get]
// @Security     BearerAuth
func GetMyPortfolioImages(w http.ResponseWriter, r *http.Request) {
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

	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT p.id, p.artisan_id, jc.id, jc.name,
		       p.image_url, p.public_id, p.caption, p.sort_order, p.created_at
		FROM artisan_portfolio_images p
		JOIN job_categories jc ON jc.id = p.category_id
		WHERE p.artisan_id = $1 AND p.category_id = $2
		ORDER BY p.sort_order ASC, p.created_at ASC
	`, userID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch portfolio images: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	images := make([]PortfolioImage, 0)
	for rows.Next() {
		var img PortfolioImage
		if err := rows.Scan(
			&img.ID, &img.ArtisanID,
			&img.Category.ID, &img.Category.Name,
			&img.ImageURL, &img.PublicID, &img.Caption,
			&img.SortOrder, &img.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		images = append(images, img)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(images),
		"data":   images,
	})
}

// ============================================================================
// GET /artisan/{id}/categories/{categoryId}/portfolio
// ============================================================================

// GetArtisanPortfolioImages godoc
// @Summary      Get an artisan's portfolio images for a category (public)
// @Description  Returns all portfolio images for a specific artisan and category.
// @Tags         Artisan Profile
// @Produce      json
// @Param        id          path  string  true  "Artisan UUID"
// @Param        categoryId  path  string     true  "Category uuid"
// @Success      200  {object}  object{status=string,count=int,data=[]PortfolioImage}
// @Router       /artisan/{id}/categories/{categoryId}/portfolio [get]
func GetArtisanPortfolioImages(w http.ResponseWriter, r *http.Request) {
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
	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT p.id, p.artisan_id, jc.id, jc.name,
		       p.image_url, p.public_id, p.caption, p.sort_order, p.created_at
		FROM artisan_portfolio_images p
		JOIN job_categories jc ON jc.id = p.category_id
		WHERE p.artisan_id = $1 AND p.category_id = $2
		ORDER BY p.sort_order ASC, p.created_at ASC
	`, artisanID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch portfolio images: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	images := make([]PortfolioImage, 0)
	for rows.Next() {
		var img PortfolioImage
		if err := rows.Scan(
			&img.ID, &img.ArtisanID,
			&img.Category.ID, &img.Category.Name,
			&img.ImageURL, &img.PublicID, &img.Caption,
			&img.SortOrder, &img.CreatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		images = append(images, img)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(images),
		"data":   images,
	})
}

// ============================================================================
// PATCH /artisan/categories/{categoryId}/portfolio/{imageId}
// ============================================================================

// UpdatePortfolioImage godoc
// @Summary      Update a portfolio image
// @Description  Updates the caption or sort_order of an existing portfolio image.
// @Tags         Artisan Profile
// @Accept       json
// @Produce      json
// @Param        categoryId  path  string     true  "Category uuid"
// @Param        imageId     path  string  true  "Image UUID"
// @Param        body        body  object{caption=string,sort_order=int}  true  "Fields to update"
// @Success      200  {object}  object{status=string,message=string,data=PortfolioImage}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan/categories/{categoryId}/portfolio/{imageId} [patch]
// @Security     BearerAuth
func UpdatePortfolioImage(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can update portfolio images", http.StatusForbidden)
		return
	}

	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}
	imageID, err := uuid.Parse(r.PathValue("imageId"))
	if err != nil {
		utils.WriteError(w, "invalid image id", http.StatusBadRequest)
		return
	}

	type request struct {
		Caption   *string `json:"caption,omitempty"`
		SortOrder *int    `json:"sort_order,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Caption == nil && req.SortOrder == nil {
		utils.WriteError(w, "nothing to update — provide caption or sort_order", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch current values
	var cur PortfolioImage
	err = db.QueryRow(ctx, `
		SELECT p.id, p.artisan_id, jc.id, jc.name,
		       p.image_url, p.public_id, p.caption, p.sort_order, p.created_at
		FROM artisan_portfolio_images p
		JOIN job_categories jc ON jc.id = p.category_id
		WHERE p.id = $1 AND p.artisan_id = $2 AND p.category_id = $3
	`, imageID, userID, categoryID).Scan(
		&cur.ID, &cur.ArtisanID,
		&cur.Category.ID, &cur.Category.Name,
		&cur.ImageURL, &cur.PublicID, &cur.Caption,
		&cur.SortOrder, &cur.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "image not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if req.Caption != nil {
		cur.Caption = req.Caption
	}
	if req.SortOrder != nil {
		cur.SortOrder = *req.SortOrder
	}

	var updated PortfolioImage
	err = db.QueryRow(ctx, `
		UPDATE artisan_portfolio_images
		SET caption = $1, sort_order = $2
		WHERE id = $3 AND artisan_id = $4 AND category_id = $5
		RETURNING id, artisan_id, category_id, image_url, public_id, caption, sort_order, created_at
	`, cur.Caption, cur.SortOrder, imageID, userID, categoryID).Scan(
		&updated.ID, &updated.ArtisanID, &updated.Category.ID,
		&updated.ImageURL, &updated.PublicID, &updated.Caption,
		&updated.SortOrder, &updated.CreatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to update portfolio image: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Reuse category info from the fetch
	updated.Category = cur.Category

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "portfolio image updated",
		"data":    updated,
	})
}

// ============================================================================
// DELETE /artisan/categories/{categoryId}/portfolio/{imageId}
// ============================================================================

// DeletePortfolioImage godoc
// @Summary      Delete a portfolio image
// @Description  Deletes a single portfolio image and removes it from Cloudinary.
// @Tags         Artisan Profile
// @Produce      json
// @Param        categoryId  path  int     true  "Category ID (integer)"
// @Param        imageId     path  string  true  "Image UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan/categories/{categoryId}/portfolio/{imageId} [delete]
// @Security     BearerAuth
func DeletePortfolioImage(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only artisans can delete portfolio images", http.StatusForbidden)
		return
	}

	categoryID, err := uuid.Parse(r.PathValue("categoryId"))
	if err != nil {
		utils.WriteError(w, "invalid category id", http.StatusBadRequest)
		return
	}
	imageID, err := uuid.Parse(r.PathValue("imageId"))
	if err != nil {
		utils.WriteError(w, "invalid image id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var publicID string
	err = db.QueryRow(ctx, `
		SELECT public_id FROM artisan_portfolio_images
		WHERE id = $1 AND artisan_id = $2 AND category_id = $3
	`, imageID, userID, categoryID).Scan(&publicID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "image not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	result, err := db.Exec(ctx, `
		DELETE FROM artisan_portfolio_images
		WHERE id = $1 AND artisan_id = $2 AND category_id = $3
	`, imageID, userID, categoryID)
	if err != nil {
		utils.Logger.Errorf("failed to delete portfolio image: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "image not found", http.StatusNotFound)
		return
	}

	if publicID != "" {
		go func(pid string) {
			cloud, err := utils.InitCloudinary()
			if err != nil {
				utils.Logger.Warnf("cloudinary init failed for cleanup: %v", err)
				return
			}
			if err := cloud.DeleteImage(context.Background(), pid); err != nil {
				utils.Logger.Warnf("failed to delete image from cloudinary (id=%s): %v", pid, err)
			}
		}(publicID)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "portfolio image deleted",
	})
}

// ============================================================================
// helpers
// ============================================================================

func pgErrCode(err error) string {
	if err == nil {
		return ""
	}
	type pgErr interface{ SQLState() string }
	if e, ok := err.(pgErr); ok {
		return e.SQLState()
	}
	return ""
}
