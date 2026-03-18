package admins

import (
	"context"
	"errors"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/jackc/pgx/v5"
)

// AdminRefreshTokenHandler godoc
// @Summary      Refresh admin access token
// @Description  Issues a new JWT for an admin using a valid refresh token cookie. Rotates the refresh token.
// @Tags         Admin Auth
// @Produce      json
// @Success      200  {object}  object{status=string,token=string}
// @Failure      401  {object}  object{error=string}
// @Router       /admin/auth/refresh [post]
func AdminRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		utils.WriteError(w, "refresh token not found", http.StatusUnauthorized)
		return
	}

	tokenHash := utils.HashRefreshToken(cookie.Value)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var tokenID string
	var adminID, fullName, role string
	var isActive bool
	var deviceType *string

	err = db.QueryRow(ctx, `
        SELECT
            art.id,
            art.device_type,
            a.id,
            a.full_name,
            a.role,
            a.is_active
        FROM admin_refresh_tokens art
        JOIN admins a ON art.admin_id = a.id
        WHERE art.token_hash = $1
          AND art.revoked_at IS NULL
          AND art.expires_at > NOW()
    `, tokenHash).Scan(
		&tokenID,
		&deviceType,
		&adminID,
		&fullName,
		&role,
		&isActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		utils.Logger.Errorf("admin refresh token db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !isActive {
		utils.WriteError(w, "your admin account has been deactivated", http.StatusForbidden)
		return
	}

	adminUUID, err := handlers.ParseAdminUUID(adminID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
        UPDATE admin_refresh_tokens SET revoked_at = NOW() WHERE id = $1
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
        INSERT INTO admin_refresh_tokens (admin_id, token_hash, device_type, expires_at)
        VALUES ($1, $2, $3, $4)
    `, adminUUID, newHash, deviceType, expiresAt)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	newAccessToken, err := utils.SignToken(adminUUID, fullName, role)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/api/v1/admin/auth/refresh",
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
		Expires:  time.Now().Add(24 * time.Hour),
		SameSite: http.SameSiteNoneMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"token":  newAccessToken,
	})
}
