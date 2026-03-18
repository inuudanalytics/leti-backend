package middlewares

import (
	"context"
	"errors"
	"fmt"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func JWTMiddleware(next http.Handler) http.Handler {
	fmt.Println("----------------- JWT middleware ------------------")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("+++++++++++ inside JWT middleware")

		var token string
		cookie, err := r.Cookie("Bearer")
		if err != nil {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			} else if queryToken := r.URL.Query().Get("token"); queryToken != "" {
				token = queryToken
			} else {
				utils.WriteError(w, "Unauthorized: Missing Bearer token", http.StatusUnauthorized)
				return
			}
		} else {
			token = strings.TrimPrefix(cookie.Value, "Bearer ")
		}

		jwtSecret := os.Getenv("JWT_SECRET")

		parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				utils.WriteError(w, "token expired", http.StatusUnauthorized)
				return
			}
			utils.WriteError(w, err.Error(), http.StatusUnauthorized)
			return
		}

		if !parsedToken.Valid {
			utils.WriteError(w, "invalid login token", http.StatusUnauthorized)
			log.Println("invalid JWT:", token)
			return
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			utils.WriteError(w, "invalid login token", http.StatusUnauthorized)
			log.Println("invalid login token:", token)
			return
		}

		uidRaw, ok := claims["uid"].(string)
		if !ok {
			utils.WriteError(w, "Invalid user ID in token", http.StatusUnauthorized)
			return
		}

		userID, err := uuid.Parse(uidRaw)
		if err != nil {
			utils.WriteError(w, "Invalid UUID format in token", http.StatusUnauthorized)
			return
		}

		// Check if account is deleted or suspended
		db := sqlconnect.DB
		var isAdmin bool
		err = db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, userID).Scan(&isAdmin)
		if err != nil {
			utils.Logger.Errorf("middleware: failed to check admin: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !isAdmin {
			// Fall back to users table
			var deletedAt *time.Time
			var status string
			err := db.QueryRow(r.Context(), `
				SELECT deleted_at, status FROM users WHERE id = $1
			`, userID).Scan(&deletedAt, &status)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					utils.WriteError(w, "account not found", http.StatusUnauthorized)
					return
				}
				utils.Logger.Errorf("middleware: failed to fetch user status: %v", err)
				utils.WriteError(w, "internal server error", http.StatusInternalServerError)
				return
			}

			if deletedAt != nil {
				utils.WriteError(w, "this account has been deleted", http.StatusUnauthorized)
				return
			}

			if status == "suspended" {
				utils.WriteError(w, "your account has been suspended. Please contact support.", http.StatusForbidden)
				return
			}
		}

		ctx := context.WithValue(r.Context(), utils.ContextKey("role"), claims["role"])
		ctx = context.WithValue(ctx, utils.ContextKey("expiresAt"), claims["exp"])
		ctx = context.WithValue(ctx, utils.ContextKey("username"), claims["user"])
		ctx = context.WithValue(ctx, utils.ContextKey("userId"), userID)

		next.ServeHTTP(w, r.WithContext(ctx))
		fmt.Println("sent response from JWT middleware")
	})
}
