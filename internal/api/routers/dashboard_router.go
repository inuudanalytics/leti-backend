package routers

import (
	"leti_server/internal/api/handlers/dashboard"
	"net/http"
)

func dashboardRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /dashboard/owners/me/dashboard", dashboard.GetOwnerDashboard)

	mux.HandleFunc("GET /dashboard/artisans/me/dashboard", dashboard.GetArtisanDashboard)

	return mux
}
