package middlewares

import (
	"leti_server/pkg/utils"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type rateLimiter struct {
	mu        sync.Mutex
	visitors  map[string]int
	limit     int
	resetTime time.Duration
}

func NewRateLimiter(limit int, resetTime time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors:  make(map[string]int),
		limit:     limit,
		resetTime: resetTime,
	}

	go rl.resetVisitorCount()

	return rl
}

func (rl *rateLimiter) resetVisitorCount() {
	ticker := time.NewTicker(rl.resetTime)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		rl.visitors = make(map[string]int)
		rl.mu.Unlock()
	}
}

func getClientIP(r *http.Request) string {
	if os.Getenv("APP_ENV") == "production" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			return strings.TrimSpace(ips[0])
		}

		if xRealIP := r.Header.Get("X-Real-IP"); xRealIP != "" {
			return strings.TrimSpace(xRealIP)
		}
	}

	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}
	return ip
}

func (rl *rateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		rl.mu.Lock()
		rl.visitors[ip]++
		count := rl.visitors[ip]
		rl.mu.Unlock()

		if count > rl.limit {
			utils.Logger.Warnf("Rate limit exceeded for IP: %s (count: %d)", ip, count)
			utils.WriteError(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
