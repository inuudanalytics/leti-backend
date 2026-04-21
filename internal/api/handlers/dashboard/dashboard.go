package dashboard

import (
	"context"
	"net/http"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
)

// ============================================================================
// GET /dashboard/owners/me/dashboard
// ============================================================================

// GetOwnerDashboard godoc
// @Summary      Owner dashboard summary cards
// @Description  Returns aggregated stats for the authenticated owner's dashboard:
//   - total_listings: all non-deleted properties owned
//   - occupancy_rate: percentage of active listing-days currently occupied (confirmed/checked_in orders today)
//   - avg_rating: average rating across all their properties
//   - occupied_shortlets: number of properties with a confirmed or checked_in order right now
//
// @Tags         Dashboard
// @Produce      json
// @Success      200  {object}  object{status=string,data=object{total_listings=int,occupancy_rate=float64,avg_rating=float64,occupied_shortlets=int}}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /dashboard/owners/me/dashboard [get]
// @Security     BearerAuth
func GetOwnerDashboard(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only owners can access this dashboard", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var totalListings int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM properties
		WHERE owner_id = $1 AND deleted_at IS NULL
	`, userID).Scan(&totalListings)

	var activeListings int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM properties
		WHERE owner_id = $1
		  AND status = 'active'
		  AND deleted_at IS NULL
	`, userID).Scan(&activeListings)

	today := time.Now().Format("2006-01-02")
	var occupiedShortlets int
	_ = db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT o.property_id)
		FROM orders o
		JOIN properties p ON p.id = o.property_id
		WHERE p.owner_id = $1
		  AND p.deleted_at IS NULL
		  AND o.status IN ('confirmed', 'checked_in')
		  AND o.check_in_date  <= $2
		  AND o.check_out_date >  $2
	`, userID, today).Scan(&occupiedShortlets)

	var occupancyRate float64
	if activeListings > 0 {
		occupancyRate = (float64(occupiedShortlets) / float64(activeListings)) * 100
	}

	var avgRating float64
	_ = db.QueryRow(ctx, `
		SELECT COALESCE(
			ROUND(
				AVG(pr.rating)::numeric,
			2),
		0)
		FROM property_reviews pr
		JOIN properties p ON p.id = pr.property_id
		WHERE p.owner_id = $1
		  AND p.deleted_at IS NULL
	`, userID).Scan(&avgRating)

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"total_listings":     totalListings,
			"occupancy_rate":     occupancyRate,
			"avg_rating":         avgRating,
			"occupied_shortlets": occupiedShortlets,
		},
	})
}

// ============================================================================
// GET /dashboard/artisans/me/dashboard
// ============================================================================

// GetArtisanDashboard godoc
// @Summary      Artisan dashboard summary cards
// @Description  Returns aggregated stats for the authenticated artisan's dashboard:
//   - completed_jobs: total bookings with status='completed'
//   - incoming_requests: pending bookings waiting for the artisan's response
//   - avg_rating: artisan's average review rating
//
// @Tags         Dashboard
// @Produce      json
// @Success      200  {object}  object{status=string,data=object{completed_jobs=int,incoming_requests=int,avg_rating=float64}}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /dashboard/artisans/me/dashboard [get]
// @Security     BearerAuth
func GetArtisanDashboard(w http.ResponseWriter, r *http.Request) {
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
	if role != "artisan" {
		utils.WriteError(w, "only artisans can access this dashboard", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// type result struct {
	// 	completedJobs    int
	// 	incomingRequests int
	// 	avgRating        float64
	// 	err              error
	// }

	completedCh := make(chan int, 1)
	incomingCh := make(chan int, 1)
	ratingCh := make(chan float64, 1)

	go func() {
		var n int
		_ = db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM artisan_bookings
			WHERE artisan_id = $1
			  AND status = 'completed'
		`, userID).Scan(&n)
		completedCh <- n
	}()

	go func() {
		var n int
		_ = db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM artisan_bookings
			WHERE artisan_id = $1
			  AND status = 'pending'
		`, userID).Scan(&n)
		incomingCh <- n
	}()

	go func() {
		var avg float64
		_ = db.QueryRow(ctx, `
			SELECT COALESCE(ROUND(AVG(rating)::numeric, 2), 0)
			FROM artisan_reviews
			WHERE artisan_id = $1
		`, userID).Scan(&avg)
		ratingCh <- avg
	}()

	completedJobs := <-completedCh
	incomingRequests := <-incomingCh
	avgRating := <-ratingCh

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"completed_jobs":    completedJobs,
			"incoming_requests": incomingRequests,
			"avg_rating":        avgRating,
		},
	})
}
