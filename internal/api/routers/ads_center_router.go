package routers

import (
	adminAdsHandler "leti_server/internal/api/handlers/admins"
	adsHandler "leti_server/internal/api/handlers/ads_center"
	mw "leti_server/internal/api/middlewares"
	"net/http"
	"time"
)

func adsRouter() *http.ServeMux {
	mux := http.NewServeMux()

	looseRL := mw.NewRateLimiter(60, time.Minute)
	moderateRL := mw.NewRateLimiter(20, time.Minute)
	tightRL := mw.NewRateLimiter(10, time.Minute)

	rl := func(limiter *mw.RateLimiter, h http.HandlerFunc) http.HandlerFunc {
		return limiter.Middleware(http.HandlerFunc(h)).ServeHTTP
	}

	// ── Public ───────────────
	mux.HandleFunc("GET /ads/pricing", rl(looseRL, adsHandler.GetAdPricing))

	// Ad event recording (view/click)
	mux.HandleFunc("POST /ads/events", rl(looseRL, adsHandler.RecordAdEvent))

	mux.HandleFunc("GET /ads/dashboard", rl(looseRL, adsHandler.GetAdsDashboard))
	mux.HandleFunc("GET /ads/campaigns", rl(looseRL, adsHandler.ListCampaigns))
	mux.HandleFunc("POST /ads/campaigns", rl(tightRL, adsHandler.CreateCampaign))
	mux.HandleFunc("GET /ads/campaigns/{id}", rl(looseRL, adsHandler.GetCampaign))
	mux.HandleFunc("PATCH /ads/campaigns/{id}/status", rl(moderateRL, adsHandler.UpdateCampaignStatus))
	mux.HandleFunc("GET /ads/campaigns/{id}/analytics", rl(looseRL, adsHandler.GetCampaignAnalytics))

	// ── Admin ────────────────
	mux.HandleFunc("PATCH /ads/admin/pricing", rl(tightRL, adminAdsHandler.AdminUpdateAdPricing))
	mux.HandleFunc("GET /ads/admin/campaigns", rl(looseRL, adminAdsHandler.AdminListCampaigns))
	mux.HandleFunc("PATCH /ads/admin/campaigns/{id}/status", rl(moderateRL, adminAdsHandler.AdminUpdateCampaignStatus))
	mux.HandleFunc("GET /ads/admin/stats", rl(looseRL, adminAdsHandler.AdminAdStats))

	return mux
}
