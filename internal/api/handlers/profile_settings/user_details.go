package profilesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

// UpdateUserProfileHandler godoc
// @Summary      Update user profile
// @Description  Updates the authenticated user's editable profile fields: bio, username. Username can only be changed once every 30 days.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{bio=string,username=string}  true  "Fields to update — all optional, send only what you want to change"
// @Success      200   {object}  object{status=string,message=string,data=object{id=string,bio=string,username=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/users/me [patch]
// @Security     BearerAuth
func UpdateUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		Bio      *string `json:"bio"`
		Username *string `json:"username"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Bio == nil && req.Username == nil {
		utils.WriteError(w, "provide at least one field to update", http.StatusBadRequest)
		return
	}

	if req.Bio != nil {
		*req.Bio = strings.TrimSpace(*req.Bio)
		if len(*req.Bio) > 200 {
			utils.WriteError(w, "bio must be at most 200 characters", http.StatusBadRequest)
			return
		}
	}

	if req.Username != nil {
		*req.Username = strings.ToLower(*req.Username)
		if *req.Username == "" {
			utils.WriteError(w, "username cannot be empty", http.StatusBadRequest)
			return
		}
		if !usernameRegex.MatchString(*req.Username) {
			utils.WriteError(w, "username must be 3–30 characters and contain only letters, numbers, or underscores", http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if req.Username != nil {
		var currentUsername string
		var usernameChangedAt *time.Time

		err := db.QueryRow(ctx, `
			SELECT COALESCE(username, ''), username_changed_at
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
		`, userID).Scan(&currentUsername, &usernameChangedAt)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "user not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("failed to fetch user for username check: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if strings.EqualFold(currentUsername, *req.Username) {
			req.Username = nil
		} else if usernameChangedAt != nil {
			nextAllowed := usernameChangedAt.Add(30 * 24 * time.Hour)
			if time.Now().Before(nextAllowed) {
				daysLeft := int(time.Until(nextAllowed).Hours()/24) + 1
				utils.WriteError(w,
					fmt.Sprintf("you can only change your username once every 30 days. You can change it again in %d day(s)", daysLeft),
					http.StatusTooManyRequests,
				)
				return
			}
		}
	}

	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Bio != nil {
		if *req.Bio == "" {
			setClauses = append(setClauses, fmt.Sprintf("bio = $%d", argIdx))
			args = append(args, nil)
		} else {
			setClauses = append(setClauses, fmt.Sprintf("bio = $%d", argIdx))
			args = append(args, *req.Bio)
		}
		argIdx++
	}

	if req.Username != nil {
		setClauses = append(setClauses, fmt.Sprintf("username = $%d", argIdx))
		args = append(args, *req.Username)
		argIdx++

		setClauses = append(setClauses, fmt.Sprintf("username_changed_at = $%d", argIdx))
		args = append(args, time.Now())
		argIdx++
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, userID)

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, COALESCE(bio, ''), COALESCE(username, '')
	`, strings.Join(setClauses, ", "), argIdx)

	var updatedID uuid.UUID
	var updatedBio, updatedUsername string

	err := db.QueryRow(ctx, query, args...).Scan(
		&updatedID, &updatedBio, &updatedUsername,
	)
	if err != nil {
		if strings.Contains(err.Error(), "users_username_key") ||
			strings.Contains(err.Error(), "unique") && strings.Contains(err.Error(), "username") {
			utils.WriteError(w, "username is already taken", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("failed to update user profile: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "profile updated successfully",
		"data": map[string]interface{}{
			"id":       updatedID,
			"bio":      updatedBio,
			"username": updatedUsername,
		},
	})
}

// ToggleArtisanOnlineStatus godoc
// @Summary      Toggle artisan online/offline status
// @Description  Allows an authenticated artisan to set their online or offline status. Only users with active_role 'artisan' and account status 'approved' can use this endpoint.
// @Tags         Artisans
// @Accept       json
// @Produce      json
// @Param        body  body  object{is_online=boolean}  true  "Online status to set"
// @Success      200   {object}  object{status=string,is_online=boolean,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      405   {object}  object{error=string}
// @Failure      500   {object}  object{error=string}
// @Router       /artisans/online-status [patch]
// @Security     BearerAuth
func ToggleArtisanOnlineStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if !isArtisan(r.Context()) {
		utils.WriteError(w, "only artisans can toggle online status", http.StatusForbidden)
		return
	}

	var body struct {
		IsOnline bool `json:"is_online"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var newStatus bool
	err := db.QueryRow(r.Context(), `
		UPDATE users
		SET is_online = $1
		WHERE id = $2 AND active_role = 'artisan' AND status = 'approved' AND deleted_at IS NULL
		RETURNING is_online
	`, body.IsOnline, userID).Scan(&newStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "artisan not found or not approved", http.StatusForbidden)
			return
		}
		utils.Logger.Errorf("failed to toggle online status: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":    "success",
		"is_online": newStatus,
		"message":   fmt.Sprintf("You are now %s", map[bool]string{true: "online", false: "offline"}[newStatus]),
	})
}

// isArtisan checks whether the authenticated user's active_role is 'artisan'.
func isArtisan(ctx context.Context) bool {
	role, ok := ctx.Value(utils.ContextKey("activeRole")).(string)
	return ok && role == "artisan"
}
