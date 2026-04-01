package profilesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UpdateUserProfileHandler godoc
// @Summary      Update user profile
// @Description  Updates the authenticated user's editable profile fields: bio.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{bio=string}  true  "Fields to update — all optional, send only what you want to change"
// @Success      200   {object}  object{status=string,message=string,data=object{id=string,bio=string}}
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
		Bio *string `json:"bio"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Bio == nil {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

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

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, userID)

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, COALESCE(bio, '')
	`, strings.Join(setClauses, ", "), argIdx)

	var updatedID uuid.UUID
	var updatedBio string

	err := db.QueryRow(ctx, query, args...).Scan(
		&updatedID, &updatedBio,
	)
	if err != nil {
		utils.Logger.Errorf("failed to update user profile: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "profile updated successfully",
		"data": map[string]interface{}{
			"id":  updatedID,
			"bio": updatedBio,
		},
	})
}
