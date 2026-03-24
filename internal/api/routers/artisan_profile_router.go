package routers

import (
	artisanprofilesettings "leti_server/internal/api/handlers/artisan_profile_settings"
	"net/http"
)

func artisanProfileRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Job categories (public) ─────────────────────────────────────────────
	mux.HandleFunc("GET /categories", artisanprofilesettings.GetAllCategories)
	mux.HandleFunc("GET /categories/{id}/variation-types", artisanprofilesettings.GetVariationTypes)

	// ── Artisan registered categories ─────────────────────────────────────────
	mux.HandleFunc("POST /artisan/categories", artisanprofilesettings.AddArtisanCategory)
	mux.HandleFunc("GET /artisan/categories", artisanprofilesettings.GetMyCategories)
	mux.HandleFunc("GET /artisans/{id}/categories", artisanprofilesettings.GetArtisanCategories)
	mux.HandleFunc("DELETE /artisan/categories/{categoryId}", artisanprofilesettings.RemoveArtisanCategory)

	// ── Portfolio images ──────────────────────────────────────────────────────
	mux.HandleFunc("POST /artisan/categories/{categoryId}/portfolio", artisanprofilesettings.AddPortfolioImages)
	mux.HandleFunc("GET /artisan/categories/{categoryId}/portfolio", artisanprofilesettings.GetMyPortfolioImages)
	mux.HandleFunc("GET /artisans/{id}/categories/{categoryId}/portfolio", artisanprofilesettings.GetArtisanPortfolioImages)
	mux.HandleFunc("PATCH /artisan/categories/{categoryId}/portfolio/{imageId}", artisanprofilesettings.UpdatePortfolioImage)
	mux.HandleFunc("DELETE /artisan/categories/{categoryId}/portfolio/{imageId}", artisanprofilesettings.DeletePortfolioImage)

	// ── Services ──────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /artisan/services", artisanprofilesettings.CreateService)
	mux.HandleFunc("GET /artisan/services", artisanprofilesettings.GetMyServices)
	mux.HandleFunc("GET /artisans/{id}/services", artisanprofilesettings.GetArtisanServices)
	mux.HandleFunc("PATCH /artisan/services/{id}", artisanprofilesettings.UpdateService)
	mux.HandleFunc("DELETE /artisan/services/{id}", artisanprofilesettings.DeleteService)

	// ── Service options ───────────────────────────────────────────────────────
	mux.HandleFunc("POST /artisan/services/{id}/options", artisanprofilesettings.AddServiceOption)
	mux.HandleFunc("GET /artisan/services/{id}/options", artisanprofilesettings.GetServiceOptions)
	mux.HandleFunc("PATCH /artisan/services/{id}/options/{optionId}", artisanprofilesettings.UpdateServiceOption)
	mux.HandleFunc("DELETE /artisan/services/{id}/options/{optionId}", artisanprofilesettings.DeleteServiceOption)

	return mux
}
