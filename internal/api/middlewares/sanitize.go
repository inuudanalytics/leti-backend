package middlewares

import (
	"fmt"
	"net/http"
)

func XSSMiddleware(next http.Handler) http.Handler {
	fmt.Println("***** initializing XSS middleware")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("***** XSS middleware ran")
		next.ServeHTTP(w, r)
		fmt.Println("***** sending response from XSS middleware")
	})
}
