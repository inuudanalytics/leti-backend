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

	apiMux.Handle("/admin/", adminRouter())

	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", apiMux))

	return mux
}
