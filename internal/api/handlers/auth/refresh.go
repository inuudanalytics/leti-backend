package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	authModels "leti_server/internal/models/auth"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/jackc/pgx/v5"
)

// RefreshTokenHandler godoc
// @Summary      Refresh access token
// @Description  Issues a new JWT access token using a valid refresh token stored in the refresh_token cookie. Also rotates the refresh token (old one is revoked, new one is issued).
// @Tags         Auth
// @Produce      json
// @Success      200  {object}  object{status=string,token=string}
// @Failure      401  {object}  object{error=string}
// @Router       /auth/refresh [post]
func RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		utils.WriteError(w, "refresh token not found", http.StatusUnauthorized)
		return
	}

	rawToken := cookie.Value
	if rawToken == "" {
		utils.WriteError(w, "refresh token is empty", http.StatusUnauthorized)
		return
	}

	tokenHash := utils.HashRefreshToken(rawToken)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var user authModels.User
	var emailNull, phoneNull sql.NullString
	var tokenID string
	var deviceType sql.NullString

	err = db.QueryRow(ctx, `
		SELECT
			rt.id,
			rt.device_type,
			u.id,
			u.first_name,
			u.last_name,
			u.username,
			u.email,
			u.phone_number,
			u.status,
			u.active_role,
			u.deleted_at
		FROM refresh_tokens rt
		JOIN users u ON rt.user_id = u.id
		WHERE rt.token_hash = $1
		  AND rt.revoked_at IS NULL
		  AND rt.expires_at > NOW()
	`, tokenHash).Scan(
		&tokenID,
		&deviceType,
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Username,
		&emailNull,
		&phoneNull,
		&user.Status,
		&user.ActiveRole,
		&user.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		utils.Logger.Errorf("refresh token db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// if emailNull.Valid {
	// 	user.Email = emailNull.String
	// }
	// if phoneNull.Valid {
	// 	user.PhoneNumber = phoneNull.String
	// }

	if user.DeletedAt != nil {
		utils.WriteError(w, "this account has been deleted", http.StatusForbidden)
		return
	}
	if user.Status == "suspended" {
		utils.WriteError(w, "your account has been suspended", http.StatusForbidden)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW() WHERE id = $1
	`, tokenID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	newRefreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	newHash := utils.HashRefreshToken(newRefreshToken)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, device_type, expires_at)
		VALUES ($1, $2, $3, $4)
	`, user.ID, newHash, deviceType, expiresAt)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := user.FirstName + " " + user.LastName
	newAccessToken, err := utils.SignToken(user.ID, displayName, user.ActiveRole)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  expiresAt,
		SameSite: http.SameSiteStrictMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    newAccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		SameSite: http.SameSiteNoneMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"token":  newAccessToken,
	})
}
