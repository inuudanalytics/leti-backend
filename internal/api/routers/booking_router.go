package routers

import (
	"leti_server/internal/api/handlers/booking"
	"net/http"
)

func bookingRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Artisan manages their own schedule
	mux.HandleFunc("POST /bookings/artisan/availability", booking.SetAvailability)
	mux.HandleFunc("DELETE /bookings/artisan/availability/{id}", booking.DeleteAvailability)
	mux.HandleFunc("GET /bookings/artisan/availability", booking.GetMyAvailability)
	mux.HandleFunc("POST /bookings/artisan/availability/overrides", booking.SetAvailabilityOverride)

	// ── Public — clients browse artisan availability before booking
	mux.HandleFunc("GET /bookings/artisans/{id}/availability", booking.GetArtisanAvailability)
	mux.HandleFunc("GET /bookings/artisans/{id}/available-slots", booking.GetAvailableSlots)

	mux.HandleFunc("GET /bookings/artisans/online", booking.GetOnlineArtisans)
	mux.HandleFunc("GET /bookings/artisans/{id}", booking.GetArtisanProfile)

	// ── Booking lifecycle
	mux.HandleFunc("POST /bookings/book", booking.CreateBooking)

	// 2. Artisan responds
	mux.HandleFunc("PATCH /bookings/{id}/confirm", booking.ConfirmBooking)
	mux.HandleFunc("PATCH /bookings/{id}/decline", booking.DeclineBooking)

	// 3. Client pays AFTER artisan confirms
	mux.HandleFunc("POST /bookings/{id}/pay", booking.InitiateBookingPayment)

	// 4. Either party cancels (refund triggered automatically if already paid)
	mux.HandleFunc("PATCH /bookings/{id}/cancel", booking.CancelBooking)

	// 5. Artisan marks completed → escrow released to artisan wallet
	mux.HandleFunc("PATCH /bookings/{id}/complete", booking.CompleteBooking)

	// ── Read endpoints
	mux.HandleFunc("GET /bookings", booking.GetMyBookings)
	mux.HandleFunc("GET /bookings/{id}", booking.GetBooking)

	return mux
}
