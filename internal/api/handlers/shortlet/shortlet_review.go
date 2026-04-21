package shortlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	shortletcache "leti_server/internal/api/handlers/shortlet/shortletcache"
	"leti_server/internal/models/shortlet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PropertyReviewResponse struct {
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
	Review  shortlet.PropertyReview `json:"review"`
}

type PropertyReviewReplyResponse struct {
	Status  string                       `json:"status"`
	Message string                       `json:"message"`
	Reply   shortlet.PropertyReviewReply `json:"reply"`
}

type PropertyReviewListResponse struct {
	Status      string                    `json:"status"`
	AvgRating   float64                   `json:"avg_rating"`
	ReviewCount int                       `json:"review_count"`
	Count       int                       `json:"count"`
	Data        []shortlet.PropertyReview `json:"data"`
	Pagination  map[string]int            `json:"pagination"`
}

// ============================================================================
// POST /shortlet/orders/{id}/reviews
// ============================================================================

// CreatePropertyReview godoc
// @Summary      Review a property after stay
// @Description  Allows a client to leave a review (rating + optional comment) for a property after their order is completed. One review per order is enforced. The property's avg_rating is updated automatically via a DB trigger.
// @Tags         Reviews
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Order UUID"
// @Param        body  body  object{rating=integer,comment=string}  true  "Rating 1–5; comment is optional"
// @Success 201 {object} PropertyReviewResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /shortlet/orders/{id}/reviews [post]
// @Security     BearerAuth
func CreatePropertyReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	clientID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok || db == nil {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if role, _ := r.Context().Value(utils.ContextKey("role")).(string); role != "client" {
		utils.WriteError(w, "only clients can review properties", http.StatusForbidden)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	type request struct {
		Rating  int     `json:"rating"`
		Comment *string `json:"comment,omitempty"`
	}
	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Rating < 1 || req.Rating > 5 {
		utils.WriteError(w, "rating must be between 1 and 5", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var propID uuid.UUID
	err = db.QueryRow(ctx, `
		SELECT property_id FROM orders
		WHERE id = $1 AND client_id = $2 AND status = 'completed'
	`, orderID, clientID).Scan(&propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found, not completed, or you did not make this booking", http.StatusForbidden)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var propOwnerID uuid.UUID
	_ = db.QueryRow(ctx, `SELECT owner_id FROM properties WHERE id = $1`, propID).Scan(&propOwnerID)
	if propOwnerID == clientID {
		utils.WriteError(w, "you cannot review your own property", http.StatusForbidden)
		return
	}

	var review shortlet.PropertyReview
	err = db.QueryRow(ctx, `
		INSERT INTO property_reviews (property_id, order_id, client_id, rating, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, property_id, order_id, client_id, rating, comment, created_at, updated_at
	`, propID, orderID, clientID, req.Rating, req.Comment).Scan(
		&review.ID, &review.PropertyID, &review.OrderID, &review.ClientID,
		&review.Rating, &review.Comment, &review.CreatedAt, &review.UpdatedAt,
	)
	if err != nil {
		if handlers.IsUniqueViolation(err) {
			utils.WriteError(w, "you have already reviewed this booking", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to create review: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go func() {
		bgCtx := context.Background()
		// Bust review + property detail caches (avg_rating changes via DB trigger)
		shortletcache.InvalidateProperty(bgCtx, propID.String())

		var ownerID uuid.UUID
		db.QueryRow(bgCtx, `SELECT owner_id FROM orders WHERE id = $1`, orderID).Scan(&ownerID)
		if ownerID != uuid.Nil {
			utils.CreateNotification(bgCtx, ownerID,
				utils.NotifReviewReceived,
				"New Property Review",
				fmt.Sprintf("Someone left a %d-star review for your property.", req.Rating),
				map[string]interface{}{"order_id": orderID, "review_id": review.ID},
			)
		}
	}()

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "review submitted",
		"review":  review,
	})
}

// ============================================================================
// POST /shortlet/reviews/{id}/reply
// ============================================================================

// ReplyToPropertyReview godoc
// @Summary      Reply to a property review
// @Description  Allows the review author (client) or the property owner to reply to a review once each. Each party may only reply once. To reply as an owner, you must be the owner of the property being reviewed.
// @Tags         Reviews
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Review UUID"
// @Param        body  body  object{body=string}  true  "Reply text"
// @Success 201 {object} PropertyReviewReplyResponse
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /shortlet/reviews/{id}/reply [post]
// @Security     BearerAuth
func ReplyToPropertyReview(w http.ResponseWriter, r *http.Request) {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "client" && role != "owner" {
		utils.WriteError(w, "only clients and owners can reply to reviews", http.StatusForbidden)
		return
	}

	reviewID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid review id", http.StatusBadRequest)
		return
	}

	type request struct {
		Body string `json:"body"`
	}
	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Body == "" {
		utils.WriteError(w, "reply body is required", http.StatusBadRequest)
		return
	}
	if len(req.Body) > 1000 {
		utils.WriteError(w, "reply cannot exceed 1000 characters", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var clientID, propOwnerID, propID uuid.UUID
	err = db.QueryRow(ctx, `
		SELECT pr.client_id, p.owner_id, p.id
		FROM property_reviews pr
		JOIN orders o ON o.id = pr.order_id
		JOIN properties p ON p.id = pr.property_id
		WHERE pr.id = $1
	`, reviewID).Scan(&clientID, &propOwnerID, &propID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "review not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	authorRole := ""
	if role == "client" && userID == clientID {
		authorRole = "client"
	} else if role == "owner" && userID == propOwnerID {
		authorRole = "owner"
	} else {
		utils.WriteError(w, "you are not authorised to reply to this review", http.StatusForbidden)
		return
	}

	var reply shortlet.PropertyReviewReply
	var authorName string
	err = db.QueryRow(ctx, `
		INSERT INTO property_review_replies (review_id, author_id, author_role, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, review_id, author_id, author_role, body, created_at
	`, reviewID, userID, authorRole, req.Body).Scan(
		&reply.ID, &reply.ReviewID, &reply.AuthorID, &reply.AuthorRole, &reply.Body, &reply.CreatedAt,
	)
	if err != nil {
		if handlers.IsUniqueViolation(err) {
			utils.WriteError(w, "you have already replied to this review", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to insert reply: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	db.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&authorName)
	reply.AuthorName = authorName

	go shortletcache.InvalidateProperty(context.Background(), propID.String())

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "reply posted",
		"reply":   reply,
	})
}

// ============================================================================
// GET /shortlet/properties/{id}/reviews
// ============================================================================

// GetPropertyReviews godoc
// @Summary      Get reviews for a property
// @Description  Returns a paginated list of reviews for a property, including any replies. Also returns the aggregate rating summary (avg, count, breakdown by star).
// @Tags         Reviews
// @Produce      json
// @Param        id     path    string  true   "Property UUID"
// @Param        page   query   integer false  "Page (default 1)"
// @Param        limit  query   integer false  "Items per page (default 10)"
// @Success 200 {object} PropertyReviewListResponse
// @Failure      404  {object}  object{error=string}
// @Router       /shortlet/properties/{id}/reviews [get]
func GetPropertyReviews(w http.ResponseWriter, r *http.Request) {
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

	page, limit := utils.GetPaginationParams(r)
	if limit > 20 {
		limit = 20
	}
	offset := (page - 1) * limit

	cacheKey := shortletcache.KeyReviews(propID.String(), page, limit)
	var cachedResult map[string]interface{}
	if hit, _ := shortletcache.GetCached(r.Context(), cacheKey, &cachedResult); hit {
		utils.WriteJSON(w, cachedResult)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var avgRating float64
	var reviewCount, star1, star2, star3, star4, star5 int
	db.QueryRow(ctx, `
		SELECT COALESCE(ROUND(AVG(rating)::numeric, 2), 0), COUNT(*),
		       COUNT(*) FILTER (WHERE rating = 1), COUNT(*) FILTER (WHERE rating = 2),
		       COUNT(*) FILTER (WHERE rating = 3), COUNT(*) FILTER (WHERE rating = 4),
		       COUNT(*) FILTER (WHERE rating = 5)
		FROM property_reviews WHERE property_id = $1
	`, propID).Scan(&avgRating, &reviewCount, &star1, &star2, &star3, &star4, &star5)

	var total int
	db.QueryRow(ctx, `SELECT COUNT(*) FROM property_reviews WHERE property_id = $1`, propID).Scan(&total)

	rows, err := db.Query(ctx, `
		SELECT
			pr.id, pr.property_id, pr.order_id, pr.client_id,
			pr.rating, pr.comment, pr.created_at, pr.updated_at,
			u.username AS reviewer_name,
			rr.id, rr.author_id, rr.author_role, rr.body, rr.created_at,
			au.username AS author_name
		FROM property_reviews pr
		JOIN users u ON u.id = pr.client_id
		LEFT JOIN property_review_replies rr ON rr.review_id = pr.id
		LEFT JOIN users au ON au.id = rr.author_id
		WHERE pr.property_id = $1
		ORDER BY pr.created_at DESC, rr.created_at ASC
		LIMIT $2 OFFSET $3
	`, propID, limit*5, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	reviewMap := make(map[uuid.UUID]*shortlet.PropertyReview)
	reviewOrder := []uuid.UUID{}

	for rows.Next() {
		var (
			rv              shortlet.PropertyReview
			reviewerName    string
			replyID         *uuid.UUID
			replyAuthorID   *uuid.UUID
			replyRole       *string
			replyBody       *string
			replyCreatedAt  *time.Time
			replyAuthorName *string
		)
		if err := rows.Scan(
			&rv.ID, &rv.PropertyID, &rv.OrderID, &rv.ClientID,
			&rv.Rating, &rv.Comment, &rv.CreatedAt, &rv.UpdatedAt,
			&reviewerName,
			&replyID, &replyAuthorID, &replyRole, &replyBody, &replyCreatedAt, &replyAuthorName,
		); err != nil {
			continue
		}
		_ = reviewerName

		if _, exists := reviewMap[rv.ID]; !exists {
			rv.Replies = []shortlet.PropertyReviewReply{}
			reviewMap[rv.ID] = &rv
			reviewOrder = append(reviewOrder, rv.ID)
		}

		if replyID != nil {
			reviewMap[rv.ID].Replies = append(reviewMap[rv.ID].Replies, shortlet.PropertyReviewReply{
				ID:         *replyID,
				ReviewID:   rv.ID,
				AuthorID:   *replyAuthorID,
				AuthorRole: *replyRole,
				AuthorName: *replyAuthorName,
				Body:       *replyBody,
				CreatedAt:  *replyCreatedAt,
			})
		}
	}

	reviews := make([]shortlet.PropertyReview, 0, len(reviewOrder))
	for _, id := range reviewOrder {
		reviews = append(reviews, *reviewMap[id])
	}

	totalPages := (total + limit - 1) / limit
	result := map[string]interface{}{
		"status":       "success",
		"avg_rating":   avgRating,
		"review_count": reviewCount,
		"breakdown": map[string]int{
			"5_star": star5, "4_star": star4, "3_star": star3,
			"2_star": star2, "1_star": star1,
		},
		"count": len(reviews),
		"data":  reviews,
		"pagination": map[string]int{
			"total": total, "page": page, "limit": limit, "total_pages": totalPages,
		},
	}

	go shortletcache.SetCached(context.Background(), cacheKey, result, shortletcache.TTLReviews)

	utils.WriteJSON(w, result)
}
