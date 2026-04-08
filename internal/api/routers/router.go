package routers

import (
	_ "leti_server/docs"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

func MainRouter() *http.ServeMux {
	mux := http.NewServeMux()

	apiMux := http.NewServeMux()

	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	apiMux.Handle("/auth/", usersRouter())

	apiMux.Handle("/notifications/", notificationsRouter())

	apiMux.Handle("/chat/", chatRouter())

	apiMux.Handle("/shortlet-chat/", shortletChatRouter())

	apiMux.Handle("/bookings/", bookingRouter())

	apiMux.Handle("/wallets/", walletRouter())

	apiMux.Handle("/webhooks/", webhookRouter())

	apiMux.Handle("/artisan-reviews/", artisanReviewsRouter())

	apiMux.Handle("/artisan/", artisanProfileRouter())

	apiMux.Handle("/profile/", profileSettingsRouter())

	apiMux.Handle("/shortlet/", shortletRouter())

	apiMux.Handle("/dashboard/", dashboardRouter())

	apiMux.Handle("/admin/", adminRouter())

	apiMux.Handle("/support/", supportRouter())

	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", apiMux))

	return mux
}
