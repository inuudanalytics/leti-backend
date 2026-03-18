package middlewares

import (
	"fmt"
	"net/http"
)

func SecurityHeaders(next http.Handler) http.Handler {
	fmt.Println("Security Headers Middleware...")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip all security headers for preflight — CORS middleware handles it
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-DNS-Prefetch-Control", "off")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1;mode=block")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Strict-Transport-Security", "max-age=63072000;includeSubDomains;preload")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Powered-By", "Django")
		w.Header().Set("Server", "")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Permissions-Policy", "geolocation=(self), microphone=()")

		// These three conflict with cross-origin requests — remove them
		// Cross-Origin-Resource-Policy: same-origin blocks cross-origin reads
		// Cross-Origin-Opener-Policy: same-origin breaks OAuth popups
		// Cross-Origin-Embedder-Policy: require-corp blocks cross-origin resources
		// Only set them if you don't need any cross-origin functionality
		// w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		// w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		// w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")

		// Content-Security-Policy: default-src 'self' blocks API calls from browser
		// — remove or relax it for an API server
		// w.Header().Set("Content-Security-Policy", "default-src 'self'")

		next.ServeHTTP(w, r)
	})
}
