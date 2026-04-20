package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"leti_server/internal/api/handlers"
	authModels "leti_server/internal/models/auth"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/api/idtoken"
)

// GoogleSignInHandler godoc
// @Summary      Google Sign-In
// @Description  Authenticates a user with a Google ID token. Creates a new account if none exists, or links to an existing account matched by google_sub or email. Wallet and user_roles are created automatically for new users.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{id_token=string,role=string}  true  "Google ID token and desired role (client, artisan, owner). Defaults to client if omitted."
// @Success      200   {object}  object{status=string,message=string,token=string,user=object{id=string,first_name=string,last_name=string,email=string,phone_number=string,status=string,active_role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/google [post]
func GoogleSignInHandler(w http.ResponseWriter, r *http.Request) {
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
		IDToken string `json:"id_token"`
		Role    string `json:"role"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.IDToken == "" {
		utils.WriteError(w, "id_token is required", http.StatusBadRequest)
		return
	}

	role := req.Role
	if !handlers.AllowedRoles[role] {
		role = "client"
	}

	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	if googleClientID == "" {
		utils.Logger.Error("GOOGLE_CLIENT_ID env variable is not set")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	payload, err := idtoken.Validate(ctx, req.IDToken, googleClientID)
	if err != nil {
		utils.Logger.Errorf("google id token validation failed: %v", err)
		utils.WriteError(w, "invalid or expired Google token", http.StatusUnauthorized)
		return
	}

	if payload.Audience != googleClientID {
		utils.WriteError(w, "token audience mismatch", http.StatusUnauthorized)
		return
	}

	sub, _ := payload.Claims["sub"].(string)
	email, _ := payload.Claims["email"].(string)
	// Google returns full name; split into first/last
	givenName, _ := payload.Claims["given_name"].(string)
	familyName, _ := payload.Claims["family_name"].(string)
	picture, _ := payload.Claims["picture"].(string)

	if givenName == "" {
		// fallback: use the full name field
		if fullName, ok := payload.Claims["name"].(string); ok {
			givenName = fullName
		}
	}
	if givenName == "" {
		givenName = "Google"
	}
	if familyName == "" {
		familyName = "User"
	}

	if sub == "" {
		utils.WriteError(w, "could not extract user identity from token", http.StatusBadRequest)
		return
	}

	dbCtx, dbCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer dbCancel()

	var user authModels.User
	var dbEmail sql.NullString
	var dbPhone sql.NullString

	err = db.QueryRow(dbCtx, `
		SELECT id, first_name, last_name, email, phone_number, status, active_role
		FROM users
		WHERE (google_sub = $1 OR (email = $2 AND $2 != ''))
		  AND deleted_at IS NULL
		LIMIT 1
	`, sub, email).Scan(&user.ID, &user.FirstName, &user.LastName, &dbEmail, &dbPhone, &user.Status, &user.ActiveRole)

	if errors.Is(err, pgx.ErrNoRows) {
		tx, txErr := db.Begin(dbCtx)
		if txErr != nil {
			utils.Logger.Errorf("failed to begin transaction: %v", txErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(dbCtx)

		var isOnline *bool
		if role == "artisan" {
			f := false
			isOnline = &f
		}

		var avatarJSON []byte
		if picture != "" {
			av := authModels.UserAvatar{URL: picture}
			avatarJSON, _ = json.Marshal(av)
		}

		insertErr := tx.QueryRow(dbCtx, `
			INSERT INTO users (
				first_name, last_name, email, email_verified, status, active_role,
				auth_provider, is_online, avatar, google_sub
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id, first_name, last_name, email, phone_number, status, active_role
		`,
			givenName,
			familyName,
			handlers.NullableString(email),
			email != "",
			handlers.RegistrationStatus(role),
			role,
			"google",
			isOnline,
			nullableBytes(avatarJSON),
			sub,
		).Scan(&user.ID, &user.FirstName, &user.LastName, &dbEmail, &dbPhone, &user.Status, &user.ActiveRole)
		if insertErr != nil {
			if pgErr, ok := insertErr.(*pgconn.PgError); ok && pgErr.Code == "23505" {
				utils.WriteError(w, "an account with this email already exists", http.StatusConflict)
				return
			}
			utils.Logger.Errorf("failed to create google user: %v", insertErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(dbCtx, `
			INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT (user_id, role) DO NOTHING
		`, user.ID, role)
		if err != nil {
			utils.Logger.Errorf("failed to insert user_role for google user: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		_, walletErr := tx.Exec(dbCtx, `
			INSERT INTO wallets (user_id, balance, currency, is_active)
			VALUES ($1, 0.00, 'NGN', TRUE) ON CONFLICT (user_id) DO NOTHING
		`, user.ID)
		if walletErr != nil {
			utils.Logger.Errorf("failed to create wallet for google user: %v", walletErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if commitErr := tx.Commit(dbCtx); commitErr != nil {
			utils.Logger.Errorf("failed to commit google user creation: %v", commitErr)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if email != "" {
			displayName := givenName + " " + familyName
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
					utils.Logger.Errorf("failed to send welcome email to google user %s: %v", e, sendErr)
				}
			}(email, displayName, role)
		}

	} else if err != nil {
		utils.Logger.Errorf("database error during google sign-in lookup: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	} else {
		_, _ = db.Exec(dbCtx, `
			UPDATE users
			SET google_sub     = COALESCE(NULLIF(google_sub, ''), $1),
			    email_verified = CASE WHEN email = $2 AND $2 != '' THEN TRUE ELSE email_verified END
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
		utils.Logger.Errorf("failed to sign token for google user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.Logger.Errorf("failed to generate refresh token for google user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := utils.StoreRefreshToken(ctx, user.ID, refreshToken, ""); err != nil {
		utils.Logger.Errorf("failed to store refresh token for google user: %v", err)
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
		"message": "google sign-in successful",
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

func nullableBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
