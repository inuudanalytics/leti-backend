package middlewares

import (
	"leti_server/pkg/utils"
	"net/http"
	"strings"
)

var allowedOrigins = []string{
	"http://localhost:3000",
}

func Cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOAuthRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if isWebhookRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")

		// Handle preflight first — before origin check so we can return
		// proper CORS error headers instead of a blank 403
		if r.Method == http.MethodOptions {
			if origin != "" && !isOriginAllowed(origin) {
				utils.WriteError(w, "Not Allowed By CORS", http.StatusForbidden)
				return
			}
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "3600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Non-preflight requests
		if origin != "" && !isOriginAllowed(origin) {
			utils.WriteError(w, "Not Allowed By CORS", http.StatusForbidden)
			return
		}

		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "3600")

		next.ServeHTTP(w, r)
	})
}

func isOAuthRoute(path string) bool {
	return strings.Contains(path, "/auth/google")
}

func isWebhookRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/webhooks/paystack")
}

func isOriginAllowed(origin string) bool {
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
