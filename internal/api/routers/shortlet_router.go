package routers

import (
	"leti_server/internal/api/handlers/shortlet"
	"net/http"
)

func shortletRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Properties ──────────────────────────────────────────────────────────
	mux.HandleFunc("GET /shortlet/properties", shortlet.ListProperties)
	mux.HandleFunc("POST /shortlet/properties", shortlet.CreateProperty)
	mux.HandleFunc("GET /shortlet/properties/{id}", shortlet.GetProperty)
	mux.HandleFunc("PATCH /shortlet/properties/{id}", shortlet.UpdateProperty)
	mux.HandleFunc("DELETE /shortlet/properties/{id}", shortlet.DeleteProperty)
	mux.HandleFunc("GET /shortlet/owners/me/properties", shortlet.GetMyProperties)

	// ── Availability & Calendar ──────────────────────────────────────────────
	mux.HandleFunc("POST /shortlet/properties/{id}/availability", shortlet.SetPropertyAvailability)
	mux.HandleFunc("DELETE /shortlet/properties/{id}/availability/{avail_id}", shortlet.DeletePropertyAvailability)
	mux.HandleFunc("POST /shortlet/properties/{id}/availability/block", shortlet.BlockPropertyDate)
	mux.HandleFunc("DELETE /shortlet/properties/{id}/availability/block/{date}", shortlet.UnblockPropertyDate)
	//  (public)
	mux.HandleFunc("GET /shortlet/properties/{id}/calendar", shortlet.GetPropertyCalendar)

	// ── Saved Listings ───────────────────────────────────────────────────────
	mux.HandleFunc("POST /shortlet/properties/{id}/save", shortlet.ToggleSavedListing)
	mux.HandleFunc("GET /shortlet/clients/me/saved-listings", shortlet.GetSavedListings)

	// ── Orders ───────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /shortlet/orders/preview", shortlet.PreviewOrder)
	mux.HandleFunc("POST /shortlet/orders", shortlet.CreateOrder)
	mux.HandleFunc("GET /shortlet/orders", shortlet.GetMyOrders)
	mux.HandleFunc("GET /shortlet/orders/{id}", shortlet.GetOrder)
	mux.HandleFunc("PATCH /shortlet/orders/{id}/cancel", shortlet.CancelOrder)
	mux.HandleFunc("PATCH /shortlet/orders/{id}/check-in", shortlet.CheckInOrder)
	mux.HandleFunc("PATCH /shortlet/orders/{id}/check-out", shortlet.CheckOutOrder)

	// ── Reviews ──────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /shortlet/orders/{id}/reviews", shortlet.CreatePropertyReview)
	mux.HandleFunc("POST /shortlet/reviews/{id}/reply", shortlet.ReplyToPropertyReview)
	//  (public)
	mux.HandleFunc("GET/shortlet/properties/{id}/reviews", shortlet.GetPropertyReviews)
	return mux
}
