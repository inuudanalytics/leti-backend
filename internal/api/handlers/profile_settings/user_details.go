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
