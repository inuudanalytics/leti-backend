package admins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// PATCH /ads/admin/pricing
// ============================================================================

// AdminUpdateAdPricing godoc
// @Summary      Admin — update ad daily prices
// @Description  Sets the daily ad price for artisans and/or property owners. Requires super_admin or admin role.
// @Tags         Admin Ads
// @Accept       json
// @Produce      json
// @Param        body  body  object{artisan_daily_price=number,owner_daily_price=number}  true  "Prices in NGN"
// @Success      200  {object}  object{status=string,message=string,data=object}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Router       /ads/admin/pricing [patch]
// @Security     BearerAuth
func AdminUpdateAdPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	adminID, ok := r.Context().Value(utils.ContextKey("adminId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		ArtisanDailyPrice *float64 `json:"artisan_daily_price"`
		OwnerDailyPrice   *float64 `json:"owner_daily_price"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ArtisanDailyPrice == nil && req.OwnerDailyPrice == nil {
		utils.WriteError(w, "at least one price field must be provided", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if req.ArtisanDailyPrice != nil {
		if *req.ArtisanDailyPrice < 0 {
			utils.WriteError(w, "artisan_daily_price must be >= 0", http.StatusBadRequest)
			return
		}
		_, err := db.Exec(ctx, `
			INSERT INTO platform_ad_pricing (target_type, daily_price, updated_by, updated_at)
			VALUES ('artisan', $1, $2, NOW())
			ON CONFLICT (target_type) DO UPDATE
				SET daily_price = EXCLUDED.daily_price,
				    updated_by  = EXCLUDED.updated_by,
				    updated_at  = NOW()
		`, *req.ArtisanDailyPrice, adminID)
		if err != nil {
			utils.Logger.Errorf("admin update artisan price: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		_, _ = db.Exec(ctx, `
			UPDATE platform_settings SET value = $1, updated_at = NOW(), updated_by = $2
			WHERE key = 'ad_daily_price_artisan'
		`, fmt.Sprintf("%.2f", *req.ArtisanDailyPrice), adminID)
	}

	if req.OwnerDailyPrice != nil {
		if *req.OwnerDailyPrice < 0 {
			utils.WriteError(w, "owner_daily_price must be >= 0", http.StatusBadRequest)
			return
		}
		_, err := db.Exec(ctx, `
			INSERT INTO platform_ad_pricing (target_type, daily_price, updated_by, updated_at)
			VALUES ('owner', $1, $2, NOW())
			ON CONFLICT (target_type) DO UPDATE
				SET daily_price = EXCLUDED.daily_price,
				    updated_by  = EXCLUDED.updated_by,
				    updated_at  = NOW()
		`, *req.OwnerDailyPrice, adminID)
		if err != nil {
			utils.Logger.Errorf("admin update owner price: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		_, _ = db.Exec(ctx, `
			UPDATE platform_settings SET value = $1, updated_at = NOW(), updated_by = $2
			WHERE key = 'ad_daily_price_owner'
		`, fmt.Sprintf("%.2f", *req.OwnerDailyPrice), adminID)
	}

	rows, err := db.Query(ctx, `SELECT target_type, daily_price, updated_at FROM platform_ad_pricing`)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	updated := map[string]interface{}{}
	for rows.Next() {
		var tType string
		var price float64
		var updatedAt time.Time
		if err := rows.Scan(&tType, &price, &updatedAt); err == nil {
			updated[tType] = map[string]interface{}{
				"daily_price": price,
				"updated_at":  updatedAt,
			}
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "ad pricing updated",
		"data":    updated,
	})
}

// ============================================================================
// GET /ads/admin/campaigns
// ============================================================================

// AdminListCampaigns godoc
// @Summary      Admin — list all campaigns
// @Description  Returns all ad campaigns across all users with pagination and optional filters.
// @Tags         Admin Ads
// @Produce      json
// @Param        status       query  string  false  "Filter by status"
// @Param        target_type  query  string  false  "artisan | owner"
// @Param        page         query  int     false  "Page (default 1)"
// @Param        limit        query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]object,pagination=object}
// @Router       /ads/admin/campaigns [get]
// @Security     BearerAuth
func AdminListCampaigns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	targetFilter := r.URL.Query().Get("target_type")
	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	args := []interface{}{}
	where := "WHERE 1=1"
	if statusFilter != "" {
		args = append(args, statusFilter)
		where += fmt.Sprintf(" AND c.status = $%d", len(args))
	}
	if targetFilter != "" {
		args = append(args, targetFilter)
		where += fmt.Sprintf(" AND c.target_type = $%d", len(args))
	}

	var total int
	_ = db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM ad_campaigns c %s`, where), args...).Scan(&total)

	args = append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT
			c.id, c.user_id,
			u.first_name || ' ' || u.last_name AS user_name,
			c.target_type, c.title, c.image_url,
			c.duration_type, c.num_days,
			c.start_date::TEXT, c.end_date::TEXT, c.mode,
			c.daily_price, c.total_budget, c.amount_spent,
			c.payment_method, c.payment_status, c.status,
			c.total_views, c.total_clicks,
			c.created_at
		FROM ad_campaigns c
		JOIN users u ON u.id = c.user_id
		%s
		ORDER BY c.created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args)), args...)
	if err != nil {
		utils.Logger.Errorf("admin list campaigns: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Row struct {
		ID            uuid.UUID `json:"id"`
		UserID        uuid.UUID `json:"user_id"`
		UserName      string    `json:"user_name"`
		TargetType    string    `json:"target_type"`
		Title         string    `json:"title"`
		ImageURL      string    `json:"image_url"`
		DurationType  string    `json:"duration_type"`
		NumDays       int       `json:"num_days"`
		StartDate     string    `json:"start_date"`
		EndDate       string    `json:"end_date"`
		Mode          string    `json:"mode"`
		DailyPrice    float64   `json:"daily_price"`
		TotalBudget   float64   `json:"total_budget"`
		AmountSpent   float64   `json:"amount_spent"`
		PaymentMethod string    `json:"payment_method"`
		PaymentStatus string    `json:"payment_status"`
		Status        string    `json:"status"`
		TotalViews    int64     `json:"total_views"`
		TotalClicks   int64     `json:"total_clicks"`
		ConvRate      float64   `json:"conversion_rate"`
		CreatedAt     time.Time `json:"created_at"`
	}

	campaigns := make([]Row, 0)
	for rows.Next() {
		var row Row
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.UserName,
			&row.TargetType, &row.Title, &row.ImageURL,
			&row.DurationType, &row.NumDays,
			&row.StartDate, &row.EndDate, &row.Mode,
			&row.DailyPrice, &row.TotalBudget, &row.AmountSpent,
			&row.PaymentMethod, &row.PaymentStatus, &row.Status,
			&row.TotalViews, &row.TotalClicks,
			&row.CreatedAt,
		); err != nil {
			utils.Logger.Errorf("admin list campaigns scan: %v", err)
			continue
		}
		if row.TotalViews > 0 {
			row.ConvRate = float64(row.TotalClicks) / float64(row.TotalViews) * 100
		}
		campaigns = append(campaigns, row)
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
// PATCH /ads/admin/campaigns/{id}/status
// ============================================================================

// AdminUpdateCampaignStatus godoc
// @Summary      Admin — force-update campaign status
// @Description  Allows admin to force-activate, suspend, or cancel any campaign.
// @Tags         Admin Ads
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Campaign UUID"
// @Param        body  body  object{status=string,reason=string}  true  "New status and optional reason"
// @Success      200  {object}  object{status=string,message=string}
// @Router       /ads/admin/campaigns/{id}/status [patch]
// @Security     BearerAuth
func AdminUpdateCampaignStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	campaignID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid campaign id", http.StatusBadRequest)
		return
	}

	type request struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	validStatuses := map[string]bool{
		"active": true, "paused": true, "auto_paused": true, "cancelled": true,
	}
	if !validStatuses[req.Status] {
		utils.WriteError(w, "invalid status", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var userID uuid.UUID
	if err := db.QueryRow(ctx,
		`UPDATE ad_campaigns SET status = $1, paused_reason = $2, updated_at = NOW() WHERE id = $3 RETURNING user_id`,
		req.Status, req.Reason, campaignID,
	).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "campaign not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(context.Background(), userID, utils.NotifGeneral,
		"Campaign Status Updated",
		fmt.Sprintf("Your ad campaign status has been updated to: %s.", req.Status),
		map[string]interface{}{"campaign_id": campaignID, "status": req.Status},
	)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("campaign status updated to %s", req.Status),
	})
}

// ============================================================================
// GET /ads/admin/stats
// ============================================================================

// AdminAdStats godoc
// @Summary      Admin — platform-wide ad statistics
// @Description  Returns platform-wide ad revenue, campaign counts, and analytics.
// @Tags         Admin Ads
// @Produce      json
// @Success      200  {object}  object{status=string,data=object}
// @Router       /ads/admin/stats [get]
// @Security     BearerAuth
func AdminAdStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var totalRevenue float64
	var totalCampaigns, activeCampaigns, artisanCampaigns, ownerCampaigns int64
	var totalViews, totalClicks int64

	_ = db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount_spent), 0),
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'active'),
			COUNT(*) FILTER (WHERE target_type = 'artisan'),
			COUNT(*) FILTER (WHERE target_type = 'owner'),
			COALESCE(SUM(total_views), 0),
			COALESCE(SUM(total_clicks), 0)
		FROM ad_campaigns
		WHERE status != 'cancelled'
	`).Scan(
		&totalRevenue, &totalCampaigns, &activeCampaigns,
		&artisanCampaigns, &ownerCampaigns,
		&totalViews, &totalClicks,
	)

	// Revenue by day (last 30 days)
	revenueRows, err := db.Query(ctx, `
		SELECT charge_date::TEXT, SUM(amount)
		FROM ad_daily_charges
		WHERE status = 'success' AND charge_date >= CURRENT_DATE - 30
		GROUP BY charge_date
		ORDER BY charge_date ASC
	`)
	type DayRevenue struct {
		Day    string  `json:"day"`
		Amount float64 `json:"amount"`
	}
	dailyRevenue := make([]DayRevenue, 0)
	if err == nil {
		defer revenueRows.Close()
		for revenueRows.Next() {
			var dr DayRevenue
			if err := revenueRows.Scan(&dr.Day, &dr.Amount); err == nil {
				dailyRevenue = append(dailyRevenue, dr)
			}
		}
	}

	convRate := 0.0
	if totalViews > 0 {
		convRate = float64(totalClicks) / float64(totalViews) * 100
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"total_revenue":     totalRevenue,
			"total_campaigns":   totalCampaigns,
			"active_campaigns":  activeCampaigns,
			"artisan_campaigns": artisanCampaigns,
			"owner_campaigns":   ownerCampaigns,
			"total_views":       totalViews,
			"total_clicks":      totalClicks,
			"conversion_rate":   convRate,
			"daily_revenue":     dailyRevenue,
		},
	})
}
