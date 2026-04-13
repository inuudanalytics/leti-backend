package routers

import (
	"leti_server/internal/api/handlers/admins"
	"leti_server/internal/api/handlers/disputes"
	"net/http"
)

func DisputeRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Job disputes ──────────────────────────────────────────
	mux.HandleFunc("POST /jobs/{id}/dispute", disputes.FileJobDispute)
	mux.HandleFunc("GET /disputes/jobs", disputes.GetMyJobDisputes)

	// ── Booking disputes ──────────────────────────────────
	mux.HandleFunc("POST /bookings/{id}/dispute", disputes.FileBookingDispute)
	mux.HandleFunc("GET /disputes/bookings", disputes.GetMyBookingDisputes)

	// ── Order (shortlet) disputes ───────────────────────────
	mux.HandleFunc("POST /orders/{id}/dispute", disputes.FileOrderDispute)
	mux.HandleFunc("GET /disputes/orders", disputes.GetMyOrderDisputes)

	// ── Dispute-linked support ticket (user view)────────────
	mux.HandleFunc("GET /disputes/ticket", admins.GetMyDisputeTicket)

	return mux
}
