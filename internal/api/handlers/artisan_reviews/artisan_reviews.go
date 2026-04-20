package artisanreviews

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func isClient(r *http.Request) bool {
	role, ok := r.Context().Value(utils.ContextKey("role")).(string)
	return ok && role == "client"
}

// ============================================================================
// POST /artisan-reviews/{id}/review  — client reviews an artisan they worked with
// ============================================================================

// LeaveReview godoc
// @Summary      Leave a review for an artisan
// @Description  A client can leave a rating and comment for an artisan after a completed and paid booking. Only one review per client per artisan — subsequent calls update the existing review.
// @Tags         Reviews
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Artisan UUID"
// @Param        body  body  object{rating=int,comment=string}  true  "rating 1–5"
// @Success      200   {object}  object{status=string,message=string,data=object{review_id=string,rating=int,avg_rating=number,total_reviews=int}}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /artisan-reviews/{id}/review [post]
// @Security     BearerAuth
func LeaveReview(w http.ResponseWriter, r *http.Request) {
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

	if !isClient(r) {
		utils.WriteError(w, "only clients can leave reviews", http.StatusForbidden)
		return
	}

	artisanID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid artisan id", http.StatusBadRequest)
		return
	}

	if userID == artisanID {
		utils.WriteError(w, "you cannot review yourself", http.StatusForbidden)
		return
	}

	type request struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Rating < 1 || req.Rating > 5 {
		utils.WriteError(w, "rating must be between 1 and 5", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var artisanUsername string
	err = db.QueryRow(ctx, `
		SELECT username FROM users
		WHERE id = $1
		  AND active_role = 'artisan'
		  AND status = 'approved'
	`, artisanID).Scan(&artisanUsername)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "artisan not found or not approved", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch artisan: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var workedTogether bool
	err = db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM artisan_bookings
			WHERE client_id    = $1
			  AND artisan_id   = $2
			  AND status       = 'completed'
			  AND payment_status = 'paid'
		)
	`, userID, artisanID).Scan(&workedTogether)
	if err != nil {
		utils.Logger.Errorf("failed to verify booking history: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !workedTogether {
		utils.WriteError(w, "you can only review an artisan after completing a paid booking with them", http.StatusForbidden)
		return
	}

	var clientUsername string
	_ = db.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&clientUsername)

	var reviewID uuid.UUID
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_reviews (artisan_id, client_id, rating, comment)
		VALUES ($1, $2, $3, NULLIF($4, ''))
		RETURNING id
	`, artisanID, userID, req.Rating, req.Comment).Scan(&reviewID)
	if err != nil {
		if handlers.PgErrCode(err) == "23505" {
			// Already reviewed — update in place
			err = db.QueryRow(ctx, `
				UPDATE artisan_reviews
				SET rating     = $1,
				    comment    = NULLIF($2, ''),
				    updated_at = NOW()
				WHERE artisan_id = $3 AND client_id = $4
				RETURNING id
			`, req.Rating, req.Comment, artisanID, userID).Scan(&reviewID)
			if err != nil {
				utils.Logger.Errorf("failed to update existing review: %v", err)
				utils.WriteError(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			utils.Logger.Errorf("failed to insert review: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	var avgRating float64
	var totalReviews int
	_ = db.QueryRow(ctx, `
		SELECT ROUND(AVG(rating)::numeric, 1), COUNT(*)
		FROM artisan_reviews
		WHERE artisan_id = $1
	`, artisanID).Scan(&avgRating, &totalReviews)

	stars := handlers.RatingStars(req.Rating)
	notifTitle := "You received a new review!"
	notifBody := fmt.Sprintf(
		"%s rated you %s (%d/5). Your average is now %.1f★ across %d review(s).",
		clientUsername, stars, req.Rating, avgRating, totalReviews,
	)
	notifData := map[string]interface{}{
		"review_id":     reviewID.String(),
		"artisan_id":    artisanID.String(),
		"rating":        req.Rating,
		"avg_rating":    avgRating,
		"total_reviews": totalReviews,
		"reviewer":      clientUsername,
	}

	go func() {
		if err := utils.CreateNotification(
			context.Background(),
			artisanID,
			utils.NotifReviewReceived,
			notifTitle,
			notifBody,
			notifData,
		); err != nil {
			utils.Logger.Errorf("failed to save review notification for artisan %s: %v", artisanID, err)
		}
	}()
	go handlers.SendPushToUser(artisanID, notifTitle, notifBody, map[string]string{
		"screen":     "Reviews",
		"review_id":  reviewID.String(),
		"artisan_id": artisanID.String(),
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "review submitted successfully",
		"data": map[string]interface{}{
			"review_id":     reviewID,
			"rating":        req.Rating,
			"avg_rating":    avgRating,
			"total_reviews": totalReviews,
		},
	})
}

// ============================================================================
// GET /artisan-reviews/{id}/reviews  — get all reviews for an artisan (public)
// ============================================================================

// GetArtisanReviews godoc
// @Summary      Get reviews for an artisan
// @Description  Returns paginated reviews for a specific artisan, including a rating summary breakdown and any replies on each review.
// @Tags         Reviews
// @Produce      json
// @Param        id     path   string  true   "Artisan UUID"
// @Param        page   query  int     false  "Page (default 1)"
// @Param        limit  query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,summary=object,count=int,data=[]object,pagination=object}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan-reviews/{id}/reviews [get]
func GetArtisanReviews(w http.ResponseWriter, r *http.Request) {
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

	var exists bool
	err = db.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND active_role = 'artisan')
	`, artisanID).Scan(&exists)
	if err != nil || !exists {
		utils.WriteError(w, "artisan not found", http.StatusNotFound)
		return
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	var avgRating float64
	var totalReviews int
	var star1, star2, star3, star4, star5 int

	err = db.QueryRow(r.Context(), `
		SELECT
			COALESCE(ROUND(AVG(rating)::numeric, 1), 0),
			COUNT(*),
			COUNT(*) FILTER (WHERE rating = 1),
			COUNT(*) FILTER (WHERE rating = 2),
			COUNT(*) FILTER (WHERE rating = 3),
			COUNT(*) FILTER (WHERE rating = 4),
			COUNT(*) FILTER (WHERE rating = 5)
		FROM artisan_reviews
		WHERE artisan_id = $1
	`, artisanID).Scan(
		&avgRating, &totalReviews,
		&star1, &star2, &star3, &star4, &star5,
	)
	if err != nil {
		utils.Logger.Errorf("failed to fetch review stats: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT
			ar.id,
			ar.rating,
			ar.comment,
			ar.created_at,
			ar.updated_at,
			reviewer.id         AS reviewer_id,
			reviewer.username AS reviewer_username,
			reviewer.avatar     AS reviewer_avatar,
			-- reply columns (NULLable)
			rr.id          AS reply_id,
			rr.author_role AS reply_author_role,
			rr.body        AS reply_body,
			rr.created_at  AS reply_created_at,
			author.id         AS reply_author_id,
			author.username AS reply_author_username,
			author.avatar     AS reply_author_avatar
		FROM artisan_reviews ar
		JOIN users reviewer ON reviewer.id = ar.client_id
		LEFT JOIN artisan_review_replies rr ON rr.review_id = ar.id
		LEFT JOIN users author ON author.id = rr.author_id
		WHERE ar.artisan_id = $1
		ORDER BY ar.created_at DESC, rr.created_at ASC
		LIMIT $2 OFFSET $3
	`, artisanID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("failed to fetch reviews: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ReplyItem struct {
		ID           uuid.UUID `json:"id"`
		AuthorID     uuid.UUID `json:"author_id"`
		AuthorName   string    `json:"author_name"`
		AuthorAvatar *string   `json:"author_avatar,omitempty"`
		AuthorRole   string    `json:"author_role"`
		Body         string    `json:"body"`
		CreatedAt    time.Time `json:"created_at"`
	}

	type ReviewItem struct {
		ID             uuid.UUID   `json:"id"`
		Rating         int         `json:"rating"`
		Comment        *string     `json:"comment,omitempty"`
		CreatedAt      time.Time   `json:"created_at"`
		UpdatedAt      time.Time   `json:"updated_at"`
		ReviewerID     uuid.UUID   `json:"reviewer_id"`
		ReviewerName   string      `json:"reviewer_name"`
		ReviewerAvatar *string     `json:"reviewer_avatar,omitempty"`
		Replies        []ReplyItem `json:"replies"`
	}

	reviewMap := make(map[uuid.UUID]*ReviewItem)
	reviewOrder := make([]uuid.UUID, 0)

	for rows.Next() {
		var (
			reviewID         uuid.UUID
			rating           int
			comment          *string
			createdAt        time.Time
			updatedAt        time.Time
			reviewerID       uuid.UUID
			reviewerUsername string
			reviewerAvatar   []byte

			replyID             *uuid.UUID
			replyAuthorRole     *string
			replyBody           *string
			replyCreatedAt      *time.Time
			replyAuthorID       *uuid.UUID
			replyAuthorUsername *string
			replyAuthorAvatar   []byte
		)

		if err := rows.Scan(
			&reviewID, &rating, &comment, &createdAt, &updatedAt,
			&reviewerID, &reviewerUsername, &reviewerAvatar,
			&replyID, &replyAuthorRole, &replyBody, &replyCreatedAt,
			&replyAuthorID, &replyAuthorUsername, &replyAuthorAvatar,
		); err != nil {
			utils.Logger.Errorf("failed to scan review row: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if _, seen := reviewMap[reviewID]; !seen {
			item := &ReviewItem{
				ID:           reviewID,
				Rating:       rating,
				Comment:      comment,
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
				ReviewerID:   reviewerID,
				ReviewerName: reviewerUsername,
				Replies:      []ReplyItem{},
			}
			if len(reviewerAvatar) > 0 {
				var av struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(reviewerAvatar, &av) == nil && av.URL != "" {
					item.ReviewerAvatar = &av.URL
				}
			}
			reviewMap[reviewID] = item
			reviewOrder = append(reviewOrder, reviewID)
		}

		if replyID != nil {
			reply := ReplyItem{
				ID:         *replyID,
				AuthorID:   *replyAuthorID,
				AuthorName: *replyAuthorUsername,
				AuthorRole: *replyAuthorRole,
				Body:       *replyBody,
				CreatedAt:  *replyCreatedAt,
			}
			if len(replyAuthorAvatar) > 0 {
				var av struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(replyAuthorAvatar, &av) == nil && av.URL != "" {
					reply.AuthorAvatar = &av.URL
				}
			}
			reviewMap[reviewID].Replies = append(reviewMap[reviewID].Replies, reply)
		}
	}

	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("review row iteration error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	reviews := make([]*ReviewItem, 0, len(reviewOrder))
	for _, id := range reviewOrder {
		reviews = append(reviews, reviewMap[id])
	}

	totalPages := (totalReviews + limit - 1) / limit

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"summary": map[string]interface{}{
			"avg_rating":    avgRating,
			"total_reviews": totalReviews,
			"breakdown": map[string]int{
				"5_star": star5,
				"4_star": star4,
				"3_star": star3,
				"2_star": star2,
				"1_star": star1,
			},
		},
		"count": len(reviews),
		"data":  reviews,
		"pagination": map[string]int{
			"total":       totalReviews,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// POST /artisan-reviews/{id}/reply  — artisan or client replies to a review (once each)
// ============================================================================

// ReplyToReview godoc
// @Summary      Reply to a review
// @Description  The artisan can reply once to any review on their profile. The client can reply once after the artisan has responded. One reply per role per review.
// @Tags         Reviews
// @Accept       json
// @Produce      json
// @Param        id    path  string  true  "Review UUID"
// @Param        body  body  object{body=string}  true  "Reply text"
// @Success      200   {object}  object{status=string,message=string,data=object}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /artisan-reviews/{id}/reply [post]
// @Security     BearerAuth
func ReplyToReview(w http.ResponseWriter, r *http.Request) {
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

	role, ok := r.Context().Value(utils.ContextKey("role")).(string)
	if !ok || (role != "artisan" && role != "client") {
		utils.WriteError(w, "only artisans and clients can reply to reviews", http.StatusForbidden)
		return
	}

	reviewID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid review id", http.StatusBadRequest)
		return
	}

	var reqBody struct {
		Body string `json:"body"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reqBody); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if reqBody.Body == "" {
		utils.WriteError(w, "reply body cannot be empty", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var artisanID, clientID uuid.UUID
	err = db.QueryRow(ctx, `
		SELECT artisan_id, client_id FROM artisan_reviews WHERE id = $1
	`, reviewID).Scan(&artisanID, &clientID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "review not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch review: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	switch role {
	case "artisan":
		if userID != artisanID {
			utils.WriteError(w, "you can only reply to reviews on your own profile", http.StatusForbidden)
			return
		}
	case "client":
		if userID != clientID {
			utils.WriteError(w, "you can only reply to your own reviews", http.StatusForbidden)
			return
		}
		var artisanReplied bool
		err = db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM artisan_review_replies
				WHERE review_id = $1 AND author_role = 'artisan'
			)
		`, reviewID).Scan(&artisanReplied)
		if err != nil {
			utils.Logger.Errorf("failed to check artisan reply: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !artisanReplied {
			utils.WriteError(w, "you can only follow up after the artisan has responded", http.StatusForbidden)
			return
		}
	}

	var replyID uuid.UUID
	var createdAt time.Time
	err = db.QueryRow(ctx, `
		INSERT INTO artisan_review_replies (review_id, author_id, author_role, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`, reviewID, userID, role, reqBody.Body).Scan(&replyID, &createdAt)
	if err != nil {
		if handlers.PgErrCode(err) == "23505" {
			utils.WriteError(w, "you have already replied to this review", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to insert reply: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var notifRecipient uuid.UUID
	var notifTitle, notifBody, authorUsername string

	_ = db.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&authorUsername)

	switch role {
	case "artisan":
		notifRecipient = clientID
		notifTitle = "The artisan replied to your review"
		notifBody = fmt.Sprintf("%s replied to your review.", authorUsername)
	case "client":
		notifRecipient = artisanID
		notifTitle = "A client followed up on their review"
		notifBody = fmt.Sprintf("%s added a follow-up to their review.", authorUsername)
	}

	notifData := map[string]interface{}{
		"reply_id":  replyID.String(),
		"review_id": reviewID.String(),
		"author":    authorUsername,
		"role":      role,
	}

	go func() {
		if err := utils.CreateNotification(
			context.Background(),
			notifRecipient,
			utils.NotifReviewReceived,
			notifTitle,
			notifBody,
			notifData,
		); err != nil {
			utils.Logger.Errorf("failed to save reply notification: %v", err)
		}
	}()
	go handlers.SendPushToUser(notifRecipient, notifTitle, notifBody, map[string]string{
		"screen":    "Reviews",
		"review_id": reviewID.String(),
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "reply posted successfully",
		"data": map[string]interface{}{
			"reply_id":    replyID,
			"review_id":   reviewID,
			"author_role": role,
			"body":        reqBody.Body,
			"created_at":  createdAt,
		},
	})
}

// ============================================================================
// GET /artisan-reviews/{id}/replies  — get all replies for a review (public)
// ============================================================================

// GetReviewReplies godoc
// @Summary      Get replies for a review
// @Description  Returns all replies on a specific review, ordered oldest first.
// @Tags         Reviews
// @Produce      json
// @Param        id  path  string  true  "Review UUID"
// @Success      200  {object}  object{status=string,count=int,data=[]object}
// @Failure      404  {object}  object{error=string}
// @Router       /artisan-reviews/{id}/replies [get]
func GetReviewReplies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	reviewID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid review id", http.StatusBadRequest)
		return
	}

	var exists bool
	err = db.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM artisan_reviews WHERE id = $1)
	`, reviewID).Scan(&exists)
	if err != nil || !exists {
		utils.WriteError(w, "review not found", http.StatusNotFound)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT
			rr.id,
			rr.author_role,
			rr.body,
			rr.created_at,
			u.id         AS author_id,
			u.username AS author_username,
			u.avatar     AS author_avatar
		FROM artisan_review_replies rr
		JOIN users u ON u.id = rr.author_id
		WHERE rr.review_id = $1
		ORDER BY rr.created_at ASC
	`, reviewID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch replies: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ReplyItem struct {
		ID           uuid.UUID `json:"id"`
		AuthorID     uuid.UUID `json:"author_id"`
		AuthorName   string    `json:"author_name"`
		AuthorAvatar *string   `json:"author_avatar,omitempty"`
		AuthorRole   string    `json:"author_role"`
		Body         string    `json:"body"`
		CreatedAt    time.Time `json:"created_at"`
	}

	replies := make([]ReplyItem, 0)
	for rows.Next() {
		var item ReplyItem
		var authorUsername string
		var avatarJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.AuthorRole,
			&item.Body,
			&item.CreatedAt,
			&item.AuthorID,
			&authorUsername,
			&avatarJSON,
		); err != nil {
			utils.Logger.Errorf("failed to scan reply row: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		item.AuthorName = authorUsername
		if len(avatarJSON) > 0 {
			var av struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(avatarJSON, &av) == nil && av.URL != "" {
				item.AuthorAvatar = &av.URL
			}
		}
		replies = append(replies, item)
	}
	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("reply row iteration error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(replies),
		"data":   replies,
	})
}
