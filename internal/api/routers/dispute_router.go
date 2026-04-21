package routers

import (
	"leti_server/internal/api/handlers/admins"
	"leti_server/internal/api/handlers/disputes"
	"net/http"
)

func disputeRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Job disputes ──────────────────────────────────────────
	mux.HandleFunc("POST /dispute-centre/jobs/{id}/dispute", disputes.FileJobDispute)
	mux.HandleFunc("GET /dispute-centre/disputes/jobs", disputes.GetMyJobDisputes)

	// ── Booking disputes ──────────────────────────────────
	mux.HandleFunc("POST /dispute-centre/bookings/{id}/dispute", disputes.FileBookingDispute)
	mux.HandleFunc("GET /dispute-centre/disputes/bookings", disputes.GetMyBookingDisputes)

	// ── Order (shortlet) disputes ───────────────────────────
	mux.HandleFunc("POST /dispute-centre/orders/{id}/dispute", disputes.FileOrderDispute)
	mux.HandleFunc("GET /dispute-centre/disputes/orders", disputes.GetMyOrderDisputes)

	// ── Dispute-linked support ticket (user view)────────────
	mux.HandleFunc("GET /dispute-centre/disputes/ticket", admins.GetMyDisputeTicket)

	return mux
}
