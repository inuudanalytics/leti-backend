package routers

import (
	"leti_server/internal/api/handlers/health"
	"net/http"
)

func healthRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /system/health", health.HealthCheck)

	return mux
}
