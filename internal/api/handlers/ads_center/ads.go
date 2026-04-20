package adscenter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	paymentwebhook "leti_server/internal/api/handlers/payment_webhook"
	"leti_server/internal/api/services"
	adsModels "leti_server/internal/models/ads_center"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// GET /ads/dashboard
// ============================================================================

// GetAdsDashboard godoc
// @Summary      Ads dashboard
// @Description  Returns aggregate metrics and active campaigns for the authenticated user (artisan or owner).
// @Tags         Ads
// @Produce      json
// @Success      200  {object}  object{status=string,data=object{metrics=object{total_views=integer,total_clicks=integer,total_spent=number,conversion_rate=number,active_campaigns=integer},active_campaigns=[]object{id=string,title=string,status=string,total_views=integer,total_clicks=integer,conversion_rate=number}}}
// @Failure      401  {object}  object{error=string}
// @Router       /ads/dashboard [get]
// @Security     BearerAuth
func GetAdsDashboard(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Aggregate metrics across all user campaigns
	var metrics adsModels.DashboardMetrics
	err := db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(total_views),  0),
			COALESCE(SUM(total_clicks), 0),
			COALESCE(SUM(amount_spent), 0),
			COUNT(*) FILTER (WHERE status = 'active')
		FROM ad_campaigns
		WHERE user_id = $1 AND status != 'cancelled'
	`, userID).Scan(
		&metrics.TotalViews,
		&metrics.TotalClicks,
		&metrics.TotalSpent,
		&metrics.ActiveCampaigns,
	)
	if err != nil {
		utils.Logger.Errorf("ads dashboard: metrics query failed: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	metrics.ConversionRate = handlers.ComputeConversionRate(metrics.TotalViews, metrics.TotalClicks)

	rows, err := db.Query(ctx, `
		SELECT
			c.id, c.user_id, c.target_type,
			c.property_id, c.artisan_service_id,
			c.title, c.description, c.image_url,
			c.duration_type, c.num_days,
			c.start_date::TEXT, c.end_date::TEXT,
			c.mode, c.daily_price, c.total_budget, c.amount_spent,
			c.payment_method, c.payment_status,
			c.status, c.total_views, c.total_clicks,
			c.paused_reason, c.last_charged_date,
			c.created_at, c.updated_at,
			-- owner fields
			p.name, p.price_per_night, p.city, p.state,
			-- artisan fields
			u.first_name || ' ' || u.last_name,
			svc.name, cat.name,
			aa.city
		FROM ad_campaigns c
		LEFT JOIN properties       p   ON p.id  = c.property_id
		LEFT JOIN artisan_services svc ON svc.id = c.artisan_service_id
		LEFT JOIN job_categories   cat ON cat.id = svc.category_id
		LEFT JOIN users            u   ON u.id   = c.user_id
		LEFT JOIN artisan_address  aa  ON aa.artisan_id = c.user_id AND aa.is_primary = TRUE
		WHERE c.user_id = $1 AND c.status IN ('active', 'paused', 'auto_paused')
		ORDER BY c.created_at DESC
	`, userID)
	if err != nil {
		utils.Logger.Errorf("ads dashboard: campaigns query failed: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var campaigns []adsModels.CampaignWithDetail
	for rows.Next() {
		var c adsModels.CampaignWithDetail
		var desc, pausedReason, lastCharged *string
		var propName, propCity, propState *string
		var artisanName, svcName, catName, artisanCity *string
		var rentPrice *float64

		if err := rows.Scan(
			&c.ID, &c.UserID, &c.TargetType,
			&c.PropertyID, &c.ArtisanServiceID,
			&c.Title, &desc, &c.ImageURL,
			&c.DurationType, &c.NumDays,
			&c.StartDate, &c.EndDate,
			&c.Mode, &c.DailyPrice, &c.TotalBudget, &c.AmountSpent,
			&c.PaymentMethod, &c.PaymentStatus,
			&c.Status, &c.TotalViews, &c.TotalClicks,
			&pausedReason, &lastCharged,
			&c.CreatedAt, &c.UpdatedAt,
			&propName, &rentPrice, &propCity, &propState,
			&artisanName, &svcName, &catName, &artisanCity,
		); err != nil {
			utils.Logger.Errorf("ads dashboard: scan error: %v", err)
			continue
		}

		c.Description = desc
		c.PausedReason = pausedReason
		c.LastChargedDate = lastCharged
		c.ConversionRate = handlers.ComputeConversionRate(c.TotalViews, c.TotalClicks)

		c.PropertyName = propName
		c.RentPrice = rentPrice
		c.PropertyCity = propCity
		c.PropertyState = propState
		c.ArtisanName = artisanName
		c.ServiceName = svcName
		c.CategoryName = catName
		c.ArtisanCity = artisanCity

		campaigns = append(campaigns, c)
	}

	if campaigns == nil {
		campaigns = []adsModels.CampaignWithDetail{}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": adsModels.Dashboard{
			Metrics:         metrics,
			ActiveCampaigns: campaigns,
		},
	})
}

// ============================================================================
// POST /ads/campaigns  (multipart/form-data)
// ============================================================================

// CreateCampaign godoc
// @Summary      Create an ad campaign
// @Description  Creates a new ad campaign for the authenticated artisan or owner. Accepts multipart/form-data with an `image` file and JSON fields. For wallet payment the first day's charge is deducted immediately. For Paystack a checkout URL is returned.
// @Tags         Ads
// @Accept       mpfd
// @Produce      json
// @Param        image             formData  file    true   "Ad banner image"
// @Param        title             formData  string  true   "Ad title (max 150 chars)"
// @Param        description       formData  string  false  "Ad description"
// @Param        duration_type     formData  string  true   "daily | weekly | biweekly | monthly"
// @Param        mode              formData  string  true   "one_time | recurring"
// @Param        start_date        formData  string  true   "YYYY-MM-DD or 'now'"
// @Param        payment_method    formData  string  true   "wallet | paystack"
// @Param        property_id       formData  string  false  "UUID of property (owners only)"
// @Param        artisan_service_id formData string  false  "UUID of artisan service (artisans only)"
// @Success      200  {object}  object{status=string,message=string,campaign=object{id=string,title=string,status=string,daily_price=number,total_budget=number,start_date=string,end_date=string}}
// @Failure      400  {object}  object{error=string}
// @Failure      402  {object}  object{error=string,code=string}
// @Router       /ads/campaigns [post]
// @Security     BearerAuth
func CreateCampaign(w http.ResponseWriter, r *http.Request) {
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
	if role != "artisan" && role != "owner" {
		utils.WriteError(w, "only artisans and property owners can create ads", http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	durationTypeStr := strings.TrimSpace(r.FormValue("duration_type"))
	modeStr := strings.TrimSpace(r.FormValue("mode"))
	startDateStr := strings.TrimSpace(r.FormValue("start_date"))
	paymentMethod := strings.TrimSpace(r.FormValue("payment_method"))
	propertyIDStr := strings.TrimSpace(r.FormValue("property_id"))
	artisanServiceIDStr := strings.TrimSpace(r.FormValue("artisan_service_id"))
	email := strings.TrimSpace(r.FormValue("email"))

	if title == "" || len(title) > 150 {
		utils.WriteError(w, "title is required and must be at most 150 characters", http.StatusBadRequest)
		return
	}

	durationType := adsModels.DurationType(durationTypeStr)
	numDays, err := handlers.DurationToDays(durationType)
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusBadRequest)
		return
	}

	mode := adsModels.CampaignMode(modeStr)
	if mode != adsModels.ModeOneTime && mode != adsModels.ModeRecurring {
		utils.WriteError(w, "mode must be 'one_time' or 'recurring'", http.StatusBadRequest)
		return
	}

	if paymentMethod != "wallet" && paymentMethod != "paystack" {
		utils.WriteError(w, "payment_method must be 'wallet' or 'paystack'", http.StatusBadRequest)
		return
	}

	var startDate time.Time
	if startDateStr == "" || startDateStr == "now" {
		startDate = time.Now().Truncate(24 * time.Hour)
	} else {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			utils.WriteError(w, "start_date must be YYYY-MM-DD or 'now'", http.StatusBadRequest)
			return
		}
		if startDate.Before(time.Now().Truncate(24 * time.Hour)) {
			utils.WriteError(w, "start_date cannot be in the past", http.StatusBadRequest)
			return
		}
	}
	endDate := startDate.AddDate(0, 0, numDays-1)

	var targetType adsModels.TargetType
	var propertyID, artisanServiceID *uuid.UUID

	if role == "owner" {
		targetType = adsModels.TargetOwner
		if propertyIDStr == "" {
			utils.WriteError(w, "property_id is required for owner ads", http.StatusBadRequest)
			return
		}
		pid, err := uuid.Parse(propertyIDStr)
		if err != nil {
			utils.WriteError(w, "invalid property_id", http.StatusBadRequest)
			return
		}
		propertyID = &pid
	} else {
		targetType = adsModels.TargetArtisan
		if artisanServiceIDStr == "" {
			utils.WriteError(w, "artisan_service_id is required for artisan ads", http.StatusBadRequest)
			return
		}
		sid, err := uuid.Parse(artisanServiceIDStr)
		if err != nil {
			utils.WriteError(w, "invalid artisan_service_id", http.StatusBadRequest)
			return
		}
		artisanServiceID = &sid
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if propertyID != nil {
		var ownerCheck uuid.UUID
		err = db.QueryRow(ctx,
			`SELECT owner_id FROM properties WHERE id = $1 AND deleted_at IS NULL`, *propertyID,
		).Scan(&ownerCheck)
		if err != nil || ownerCheck != userID {
			utils.WriteError(w, "property not found or does not belong to you", http.StatusForbidden)
			return
		}
	}
	if artisanServiceID != nil {
		var svcOwner uuid.UUID
		err = db.QueryRow(ctx,
			`SELECT artisan_id FROM artisan_services WHERE id = $1 AND is_active = TRUE`, *artisanServiceID,
		).Scan(&svcOwner)
		if err != nil || svcOwner != userID {
			utils.WriteError(w, "service not found or does not belong to you", http.StatusForbidden)
			return
		}
	}

	var dailyPrice float64
	err = db.QueryRow(ctx,
		`SELECT daily_price FROM platform_ad_pricing WHERE target_type = $1`, string(targetType),
	).Scan(&dailyPrice)
	if err != nil {
		utils.Logger.Errorf("failed to fetch ad pricing: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	totalBudget := dailyPrice * float64(numDays)

	file, header, err := r.FormFile("image")
	if err != nil {
		utils.WriteError(w, "ad image is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	cloud, err := utils.InitCloudinary()
	if err != nil {
		utils.WriteError(w, "image upload service unavailable", http.StatusInternalServerError)
		return
	}

	uploadFiles := []utils.UploadFile{{Reader: file, Filename: header.Filename}}
	urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, uploadFiles, "ads")
	if err != nil || len(urls) == 0 {
		utils.WriteError(w, "failed to upload ad image", http.StatusInternalServerError)
		return
	}
	imageURL := urls[0]
	imagePublicID := publicIDs[0]

	var desc *string
	if description != "" {
		desc = &description
	}

	var campaignID uuid.UUID
	err = db.QueryRow(ctx, `
		INSERT INTO ad_campaigns (
			user_id, target_type,
			property_id, artisan_service_id,
			title, description,
			image_url, image_public_id,
			duration_type, num_days,
			start_date, end_date, mode,
			daily_price, total_budget,
			payment_method, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'pending')
		RETURNING id
	`,
		userID, string(targetType),
		propertyID, artisanServiceID,
		title, desc,
		imageURL, imagePublicID,
		string(durationType), numDays,
		startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), string(mode),
		dailyPrice, totalBudget,
		paymentMethod,
	).Scan(&campaignID)
	if err != nil {
		handlers.CleanupUploads(ctx, cloud, publicIDs)
		utils.Logger.Errorf("failed to insert campaign: %v", err)
		utils.WriteError(w, "failed to create campaign", http.StatusInternalServerError)
		return
	}

	switch paymentMethod {
	case "wallet":
		if err := paymentwebhook.ProcessWalletAdPayment(ctx, db, userID, campaignID, dailyPrice, startDate); err != nil {
			utils.Logger.Errorf("wallet ad payment failed for campaign %s: %v", campaignID, err)
			utils.WriteJSON(w, map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
				"code":    "INSUFFICIENT_BALANCE",
			})
			return
		}
	case "paystack":
		var dbEmail *string
		_ = db.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&dbEmail)
		paystackEmail := ""
		if dbEmail != nil && *dbEmail != "" {
			paystackEmail = *dbEmail
		} else if email != "" {
			paystackEmail = email
		} else {
			utils.WriteError(w, "no email address found — please provide one for Paystack payment", http.StatusBadRequest)
			return
		}

		ps, err := services.NewPaystackClient()
		if err != nil {
			utils.WriteError(w, "payment service unavailable", http.StatusInternalServerError)
			return
		}
		amountKobo := int64(totalBudget * 100)
		res, err := ps.InitializePayment(map[string]interface{}{
			"email":  paystackEmail,
			"amount": amountKobo,
			"metadata": map[string]interface{}{
				"user_id":          userID.String(),
				"campaign_id":      campaignID.String(),
				"transaction_type": "ad_payment",
			},
		})
		if err != nil {
			utils.Logger.Errorf("Paystack init for ad campaign %s: %v", campaignID, err)
			utils.WriteError(w, "failed to initialize Paystack payment", http.StatusBadRequest)
			return
		}
		utils.WriteJSON(w, map[string]interface{}{
			"status":      "success",
			"message":     "Paystack payment initialized — complete payment to activate your campaign",
			"campaign_id": campaignID,
			"payment":     res,
		})
		return
	}

	campaign, err := handlers.FetchCampaignByID(ctx, db, campaignID)
	if err != nil {
		utils.WriteJSON(w, map[string]interface{}{
			"status":      "success",
			"message":     "campaign created successfully",
			"campaign_id": campaignID,
		})
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  "ad campaign created and activated",
		"campaign": campaign,
	})
}

// ============================================================================
// GET /ads/campaigns
// ============================================================================

// ListCampaigns godoc
// @Summary      List user campaigns
// @Description  Returns all campaigns for the authenticated user with pagination. Filter by status using ?status=active.
// @Tags         Ads
// @Produce      json
// @Param        status  query  string  false  "Filter by status: pending|active|paused|auto_paused|completed|cancelled"
// @Param        page    query  int     false  "Page (default 1)"
// @Param        limit   query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]object{id=string,title=string,target_type=string,status=string,total_views=integer,total_clicks=integer,conversion_rate=number,daily_price=number,amount_spent=number,start_date=string,end_date=string},pagination=object{total=integer,page=integer,limit=integer,total_pages=integer}}
// @Router       /ads/campaigns [get]
// @Security     BearerAuth
func ListCampaigns(w http.ResponseWriter, r *http.Request) {
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

	statusFilter := r.URL.Query().Get("status")
	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	args := []interface{}{userID}
	whereClause := "WHERE c.user_id = $1"
	if statusFilter != "" {
		args = append(args, statusFilter)
		whereClause += fmt.Sprintf(" AND c.status = $%d", len(args))
	}

	var total int
	_ = db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM ad_campaigns c %s`, whereClause), args...).Scan(&total)

	args = append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT
			c.id, c.user_id, c.target_type,
			c.property_id, c.artisan_service_id,
			c.title, c.description, c.image_url,
			c.duration_type, c.num_days,
			c.start_date::TEXT, c.end_date::TEXT,
			c.mode, c.daily_price, c.total_budget, c.amount_spent,
			c.payment_method, c.payment_status,
			c.status, c.total_views, c.total_clicks,
			c.paused_reason, c.last_charged_date,
			c.created_at, c.updated_at,
			p.name, p.price_per_night, p.city, p.state,
			u.first_name || ' ' || u.last_name,
			svc.name, cat.name, aa.city
		FROM ad_campaigns c
		LEFT JOIN properties       p   ON p.id  = c.property_id
		LEFT JOIN artisan_services svc ON svc.id = c.artisan_service_id
		LEFT JOIN job_categories   cat ON cat.id = svc.category_id
		LEFT JOIN users            u   ON u.id   = c.user_id
		LEFT JOIN artisan_address  aa  ON aa.artisan_id = c.user_id AND aa.is_primary = TRUE
		%s
		ORDER BY c.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args)), args...)
	if err != nil {
		utils.Logger.Errorf("list campaigns: query error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	campaigns := make([]adsModels.CampaignWithDetail, 0)
	for rows.Next() {
		var c adsModels.CampaignWithDetail
		var desc, pausedReason, lastCharged *string
		var propName, propCity, propState *string
		var artisanName, svcName, catName, artisanCity *string
		var rentPrice *float64

		if err := rows.Scan(
			&c.ID, &c.UserID, &c.TargetType,
			&c.PropertyID, &c.ArtisanServiceID,
			&c.Title, &desc, &c.ImageURL,
			&c.DurationType, &c.NumDays,
			&c.StartDate, &c.EndDate,
			&c.Mode, &c.DailyPrice, &c.TotalBudget, &c.AmountSpent,
			&c.PaymentMethod, &c.PaymentStatus,
			&c.Status, &c.TotalViews, &c.TotalClicks,
			&pausedReason, &lastCharged,
			&c.CreatedAt, &c.UpdatedAt,
			&propName, &rentPrice, &propCity, &propState,
			&artisanName, &svcName, &catName, &artisanCity,
		); err != nil {
			utils.Logger.Errorf("list campaigns: scan error: %v", err)
			continue
		}
		c.Description = desc
		c.PausedReason = pausedReason
		c.LastChargedDate = lastCharged
		c.ConversionRate = handlers.ComputeConversionRate(c.TotalViews, c.TotalClicks)
		c.PropertyName = propName
		c.RentPrice = rentPrice
		c.PropertyCity = propCity
		c.PropertyState = propState
		c.ArtisanName = artisanName
		c.ServiceName = svcName
		c.CategoryName = catName
		c.ArtisanCity = artisanCity
		campaigns = append(campaigns, c)
	}

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(campaigns),
		"data":   campaigns,
		"pagination": map[string]int{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /ads/campaigns/{id}
// ============================================================================

// GetCampaign godoc
// @Summary      Get campaign details
// @Description  Returns full details of a single campaign including daily charge history.
// @Tags         Ads
// @Produce      json
// @Param        id  path  string  true  "Campaign UUID"
// @Success      200  {object}  object{status=string,data=object{id=string,title=string,status=string,target_type=string,total_views=integer,total_clicks=integer,conversion_rate=number,daily_price=number,total_budget=number,amount_spent=number,start_date=string,end_date=string,mode=string,payment_method=string,payment_status=string},charges=[]object{id=string,charge_date=string,amount=number,status=string,failure_reason=string}}
// @Router       /ads/campaigns/{id} [get]
// @Security     BearerAuth
func GetCampaign(w http.ResponseWriter, r *http.Request) {
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

	campaignID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid campaign id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	campaign, err := handlers.FetchCampaignByID(ctx, db, campaignID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "campaign not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if campaign.UserID != userID {
		utils.WriteError(w, "you do not own this campaign", http.StatusForbidden)
		return
	}

	chargeRows, err := db.Query(ctx, `
		SELECT id, campaign_id, charge_date::TEXT, amount, status, failure_reason, created_at
		FROM ad_daily_charges
		WHERE campaign_id = $1
		ORDER BY charge_date DESC
	`, campaignID)
	if err != nil {
		utils.Logger.Errorf("get campaign charges: %v", err)
	}
	defer func() {
		if chargeRows != nil {
			chargeRows.Close()
		}
	}()

	charges := make([]adsModels.DailyCharge, 0)
	if chargeRows != nil {
		for chargeRows.Next() {
			var ch adsModels.DailyCharge
			var failReason *string
			if err := chargeRows.Scan(&ch.ID, &ch.CampaignID, &ch.ChargeDate, &ch.Amount, &ch.Status, &failReason, &ch.CreatedAt); err == nil {
				ch.FailureReason = failReason
				charges = append(charges, ch)
			}
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"data":    campaign,
		"charges": charges,
	})
}

// ============================================================================
// PATCH /ads/campaigns/{id}/status
// ============================================================================

// UpdateCampaignStatus godoc
// @Summary      Update campaign status
// @Description  Allows the user to suspend, resume, or cancel a campaign. Admin auto-pauses are handled by the system.
// @Tags         Ads
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Campaign UUID"
// @Param        body  body  object{action=string}  true  "action: suspend | resume | cancel"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Router       /ads/campaigns/{id}/status [patch]
// @Security     BearerAuth
func UpdateCampaignStatus(w http.ResponseWriter, r *http.Request) {
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

	campaignID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid campaign id", http.StatusBadRequest)
		return
	}

	type request struct {
		Action string `json:"action"` // suspend | resume | cancel
	}
	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Action != "suspend" && req.Action != "resume" && req.Action != "cancel" {
		utils.WriteError(w, "action must be 'suspend', 'resume', or 'cancel'", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var currentStatus adsModels.CampaignStatus
	var campaignUserID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT user_id, status FROM ad_campaigns WHERE id = $1`, campaignID,
	).Scan(&campaignUserID, &currentStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "campaign not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if campaignUserID != userID {
		utils.WriteError(w, "you do not own this campaign", http.StatusForbidden)
		return
	}

	var newStatus adsModels.CampaignStatus
	var msg string

	switch req.Action {
	case "suspend":
		if currentStatus != adsModels.StatusActive {
			utils.WriteError(w, "only active campaigns can be suspended", http.StatusBadRequest)
			return
		}
		newStatus = adsModels.StatusPaused
		msg = "campaign suspended"
	case "resume":
		if currentStatus != adsModels.StatusPaused && currentStatus != adsModels.StatusAutoPaused {
			utils.WriteError(w, "only paused campaigns can be resumed", http.StatusBadRequest)
			return
		}
		newStatus = adsModels.StatusActive
		msg = "campaign resumed"
	case "cancel":
		if currentStatus == adsModels.StatusCancelled || currentStatus == adsModels.StatusCompleted {
			utils.WriteError(w, "campaign is already ended", http.StatusBadRequest)
			return
		}
		newStatus = adsModels.StatusCancelled
		msg = "campaign cancelled"
	}

	_, err = db.Exec(ctx,
		`UPDATE ad_campaigns SET status = $1, paused_reason = NULL, updated_at = NOW() WHERE id = $2`,
		string(newStatus), campaignID,
	)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(ctx, userID, utils.NotifGeneral,
		"Campaign Updated",
		fmt.Sprintf("Your ad campaign has been %s.", req.Action+"d"),
		map[string]interface{}{"campaign_id": campaignID, "new_status": string(newStatus)},
	)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": msg,
	})
}

// ============================================================================
// POST /ads/events  — record impressions and clicks
// ============================================================================

// RecordAdEvent godoc
// @Summary      Record an ad impression or click
// @Description  Records a view or click event for an ad campaign. Used by the frontend when an ad is displayed or interacted with. Viewer is extracted from the auth token if provided.
// @Tags         Ads
// @Accept       json
// @Produce      json
// @Param        body  body  object{campaign_id=string,event_type=string}  true  "event_type: view | click"
// @Success      200  {object}  object{status=string}
// @Router       /ads/events [post]
func RecordAdEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		CampaignID string `json:"campaign_id"`
		EventType  string `json:"event_type"` // view | click
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	campaignID, err := uuid.Parse(req.CampaignID)
	if err != nil {
		utils.WriteError(w, "invalid campaign_id", http.StatusBadRequest)
		return
	}
	if req.EventType != "view" && req.EventType != "click" {
		utils.WriteError(w, "event_type must be 'view' or 'click'", http.StatusBadRequest)
		return
	}

	// Optional viewer (token may or may not be present)
	var viewerID *uuid.UUID
	if uid, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID); ok {
		viewerID = &uid
	}

	ipAddress := r.Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
	}
	userAgent := r.UserAgent()

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := db.Exec(bgCtx, `
			INSERT INTO ad_events (campaign_id, viewer_id, event_type, ip_address, user_agent)
			VALUES ($1, $2, $3, $4, $5)
		`, campaignID, viewerID, req.EventType, ipAddress, userAgent)
		if err != nil {
			utils.Logger.Warnf("failed to record ad event: %v", err)
		}
	}()

	utils.WriteJSON(w, map[string]interface{}{"status": "success"})
}

// ============================================================================
// GET /ads/pricing  — fetch current daily prices
// ============================================================================

// GetAdPricing godoc
// @Summary      Get current ad pricing
// @Description  Returns the admin-configured daily ad prices for artisans and property owners, along with pre-computed totals per duration.
// @Tags         Ads
// @Produce      json
// @Success      200  {object}  object{status=string,data=object}
// @Router       /ads/pricing [get]
func GetAdPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `SELECT target_type, daily_price FROM platform_ad_pricing`)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	pricing := map[string]interface{}{}
	for rows.Next() {
		var tType string
		var price float64
		if err := rows.Scan(&tType, &price); err != nil {
			continue
		}
		pricing[tType] = map[string]interface{}{
			"daily_price":    price,
			"weekly_total":   price * 7,
			"biweekly_total": price * 14,
			"monthly_total":  price * 30,
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data":   pricing,
	})
}

// ============================================================================
// GET /ads/campaigns/{id}/analytics
// ============================================================================

// GetCampaignAnalytics godoc
// @Summary      Campaign analytics
// @Description  Returns daily view/click breakdown for a campaign over its lifetime.
// @Tags         Ads
// @Produce      json
// @Param        id  path  string  true  "Campaign UUID"
// @Success      200  {object}  object{status=string,data=object}
// @Router       /ads/campaigns/{id}/analytics [get]
// @Security     BearerAuth
func GetCampaignAnalytics(w http.ResponseWriter, r *http.Request) {
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

	campaignID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid campaign id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Ownership check
	var ownerID uuid.UUID
	if err := db.QueryRow(ctx, `SELECT user_id FROM ad_campaigns WHERE id = $1`, campaignID).Scan(&ownerID); err != nil {
		utils.WriteError(w, "campaign not found", http.StatusNotFound)
		return
	}
	if ownerID != userID {
		utils.WriteError(w, "you do not own this campaign", http.StatusForbidden)
		return
	}

	rows, err := db.Query(ctx, `
		SELECT
			DATE(created_at)::TEXT AS day,
			COUNT(*) FILTER (WHERE event_type = 'view')  AS views,
			COUNT(*) FILTER (WHERE event_type = 'click') AS clicks
		FROM ad_events
		WHERE campaign_id = $1
		GROUP BY DATE(created_at)
		ORDER BY day ASC
	`, campaignID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type DayStat struct {
		Day    string  `json:"day"`
		Views  int64   `json:"views"`
		Clicks int64   `json:"clicks"`
		CTR    float64 `json:"ctr"`
	}

	stats := make([]DayStat, 0)
	for rows.Next() {
		var ds DayStat
		if err := rows.Scan(&ds.Day, &ds.Views, &ds.Clicks); err == nil {
			ds.CTR = handlers.ComputeConversionRate(ds.Views, ds.Clicks)
			stats = append(stats, ds)
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data":   stats,
	})
}
