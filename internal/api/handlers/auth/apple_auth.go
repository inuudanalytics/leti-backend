package auth

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	authModels "leti_server/internal/models/auth"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type appleJWKS struct {
	Keys []appleJWK `json:"keys"`
}

type appleJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// AppleSignInHandler godoc
// @Summary      Apple Sign-In
// @Description  Authenticates a user with an Apple identity token. Creates a new account if none exists. Apple only sends email and name on the very first sign-in — the client should pass them in the request body as a fallback for subsequent logins.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{identity_token=string,first_name=string,last_name=string,email=string,role=string}  true  "Apple identity token. first_name, last_name, and email are only available on first sign-in from Apple."
// @Success      200   {object}  object{status=string,message=string,token=string,user=object{id=string,first_name=string,last_name=string,email=string,phone_number=string,status=string,active_role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/apple [post]
func AppleSignInHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("DB is not initialized")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		IdentityToken string `json:"identity_token"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		Email         string `json:"email"`
		Role          string `json:"role"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.IdentityToken == "" {
		utils.WriteError(w, "identity_token is required", http.StatusBadRequest)
		return
	}

	role := req.Role
	if !handlers.AllowedRoles[role] {
		role = "client"
	}

	appleAudience := os.Getenv("APPLE_AUDIENCE")
	if appleAudience == "" {
		utils.Logger.Error("APPLE_AUDIENCE env variable is not set")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	appleIssuer := os.Getenv("APPLE_ISSUER")
	if appleIssuer == "" {
		appleIssuer = "https://appleid.apple.com"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	claims, err := verifyAppleToken(ctx, req.IdentityToken, appleAudience, appleIssuer)
	if err != nil {
		utils.Logger.Errorf("apple token verification failed: %v", err)
		utils.WriteError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	sub, _ := claims["sub"].(string)
	tokenEmail, _ := claims["email"].(string)

	if sub == "" {
		utils.WriteError(w, "could not extract user identity from token", http.StatusBadRequest)
		return
	}

	email := tokenEmail
	if email == "" {
		email = req.Email
	}

	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)

	dbCtx, dbCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer dbCancel()

	var user authModels.User
	var dbEmail sql.NullString
	var dbPhone sql.NullString

	err = db.QueryRow(dbCtx, `
		SELECT id, first_name, last_name, email, phone_number, status, active_role
		FROM users
		WHERE (apple_sub = $1 OR (email = $2 AND $2 != '' AND $2 IS NOT NULL))
		  AND deleted_at IS NULL
		LIMIT 1
	`, sub, handlers.NullableString(email)).
		Scan(&user.ID, &user.FirstName, &user.LastName, &dbEmail, &dbPhone, &user.Status, &user.ActiveRole)

	if errors.Is(err, pgx.ErrNoRows) {
		tx, txErr := db.Begin(dbCtx)
		if txErr != nil {
			utils.Logger.Errorf("failed to begin transaction: %v", txErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(dbCtx)

		// Apple only sends name on first sign-in
		if firstName == "" {
			firstName = "Apple"
		}
		if lastName == "" {
			lastName = "User"
		}

		var isOnline *bool
		if role == "artisan" {
			f := false
			isOnline = &f
		}

		insertErr := tx.QueryRow(dbCtx, `
			INSERT INTO users (
				first_name, last_name, email, email_verified, status, active_role,
				auth_provider, is_online, apple_sub
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id, first_name, last_name, email, phone_number, status, active_role
		`,
			firstName,
			lastName,
			handlers.NullableString(email),
			email != "",
			handlers.RegistrationStatus(role),
			role,
			"apple",
			isOnline,
			sub,
		).Scan(&user.ID, &user.FirstName, &user.LastName, &dbEmail, &dbPhone, &user.Status, &user.ActiveRole)
		if insertErr != nil {
			if pgErr, ok := insertErr.(*pgconn.PgError); ok && pgErr.Code == "23505" {
				utils.WriteError(w, "an account with this email already exists", http.StatusConflict)
				return
			}
			utils.Logger.Errorf("failed to create apple user: %v", insertErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(dbCtx, `
			INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT (user_id, role) DO NOTHING
		`, user.ID, role)
		if err != nil {
			utils.Logger.Errorf("failed to insert user_role for apple user: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		_, walletErr := tx.Exec(dbCtx, `
			INSERT INTO wallets (user_id, balance, currency, is_active)
			VALUES ($1, 0.00, 'NGN', TRUE) ON CONFLICT (user_id) DO NOTHING
		`, user.ID)
		if walletErr != nil {
			utils.Logger.Errorf("failed to create wallet for apple user: %v", walletErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if commitErr := tx.Commit(dbCtx); commitErr != nil {
			utils.Logger.Errorf("failed to commit apple user creation: %v", commitErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if email != "" {
			displayName := firstName + " " + lastName
			go func(e, n, rl string) {
				var sendErr error
				switch rl {
				case "client":
					sendErr = utils.SendWelcomeEmailClient(e, n)
				case "owner":
					sendErr = utils.SendWelcomeEmailOwner(e, n)
				case "artisan":
					sendErr = utils.SendWelcomeEmailArtisan(e, n)
				}
				if sendErr != nil {
					utils.Logger.Errorf("failed to send welcome email to apple user %s: %v", e, sendErr)
				}
			}(email, displayName, role)
		}

	} else if err != nil {
		utils.Logger.Errorf("database error during apple sign-in lookup: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	} else {
		_, _ = db.Exec(dbCtx, `
			UPDATE users
			SET apple_sub     = COALESCE(NULLIF(apple_sub, ''), $1),
			    email         = COALESCE(email, NULLIF($2, '')),
			    email_verified = CASE WHEN email IS NULL AND $2 != '' THEN TRUE ELSE email_verified END
			WHERE id = $3
		`, sub, email, user.ID)
	}

	if dbEmail.Valid {
		user.Email = dbEmail.String
	}
	if dbPhone.Valid {
		user.PhoneNumber = dbPhone.String
	}

	if user.Status == "suspended" {
		utils.WriteError(w, "your account has been suspended, please contact support", http.StatusForbidden)
		return
	}

	displayName := user.FirstName + " " + user.LastName
	tokenString, err := utils.SignToken(user.ID, displayName, user.ActiveRole)
	if err != nil {
		utils.Logger.Errorf("failed to sign token for apple user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.Logger.Errorf("failed to generate refresh token for apple user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := utils.StoreRefreshToken(dbCtx, user.ID, refreshToken, ""); err != nil {
		utils.Logger.Errorf("failed to store refresh token for apple user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		SameSite: http.SameSiteNoneMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		SameSite: http.SameSiteStrictMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "apple sign-in successful",
		"token":   tokenString,
		"user": map[string]interface{}{
			"id":           user.ID,
			"first_name":   user.FirstName,
			"last_name":    user.LastName,
			"email":        user.Email,
			"phone_number": user.PhoneNumber,
			"status":       user.Status,
			"active_role":  user.ActiveRole,
		},
	})
}

// ── Apple token verification ──────────────────────────────────────────────────

func verifyAppleToken(ctx context.Context, tokenStr, audience, issuer string) (jwt.MapClaims, error) {
	appleJWKSURL := os.Getenv("APPLE_JWKS_URL")
	if appleJWKSURL == "" {
		appleJWKSURL = "https://appleid.apple.com/auth/keys"
	}

	jwks, err := fetchAppleJWKS(ctx, appleJWKSURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Apple JWKS: %w", err)
	}

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		pubKey, keyErr := findApplePublicKey(jwks, kid)
		if keyErr != nil {
			return nil, keyErr
		}
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	iss, _ := claims["iss"].(string)
	if iss != issuer {
		return nil, fmt.Errorf("invalid issuer: %q", iss)
	}
	if !audienceMatches(claims["aud"], audience) {
		return nil, fmt.Errorf("token audience does not match %q", audience)
	}

	return claims, nil
}

func audienceMatches(raw interface{}, audience string) bool {
	switch v := raw.(type) {
	case string:
		return v == audience
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == audience {
				return true
			}
		}
	}
	return false
}

func fetchAppleJWKS(ctx context.Context, url string) (*appleJWKS, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jwks appleJWKS
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, err
	}
	return &jwks, nil
}

func findApplePublicKey(jwks *appleJWKS, kid string) (*rsa.PublicKey, error) {
	for _, key := range jwks.Keys {
		if key.Kid == kid {
			return jwkToRSAPublicKey(key)
		}
	}
	for _, key := range jwks.Keys {
		if key.Alg == "RS256" {
			return jwkToRSAPublicKey(key)
		}
	}
	return nil, fmt.Errorf("no matching Apple public key found for kid=%q", kid)
}

func jwkToRSAPublicKey(key appleJWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key E: %w", err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}
