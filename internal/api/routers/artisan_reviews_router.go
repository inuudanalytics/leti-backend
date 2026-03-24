package routers

import (
	artisanreviews "leti_server/internal/api/handlers/artisan_reviews"
	"net/http"
)

func artisanReviewsRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /artisan-reviews/{id}/review", artisanreviews.LeaveReview)

	mux.HandleFunc("GET /artisan-reviews/{id}/reviews", artisanreviews.GetArtisanReviews)

	mux.HandleFunc("GET /artisan-reviews/{id}/replies", artisanreviews.GetReviewReplies)

	mux.HandleFunc("POST /artisan-reviews/{id}/reply", artisanreviews.ReplyToReview)

	return mux
}
