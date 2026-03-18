package admins

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	adminModels "leti_server/internal/models/admins"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/argon2"
)

// ============================================================================
// POST /admin/auth/login
// ============================================================================

// AdminLoginHandler godoc
// @Summary      Admin login
// @Description  Authenticates an admin with email and password. Returns a JWT token valid for 24 hours.
// @Tags         Admin Auth
// @Accept       json
// @Produce      json
// @Param        body  body  adminModels.AdminLoginRequest  true  "Admin credentials"
// @Success      200   {object}  object{status=string,message=string,token=string,admin=object{id=string,full_name=string,email=string,role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Router       /admin/auth/login [post]
func AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var req adminModels.AdminLoginRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		utils.WriteError(w, "email and password are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var admin adminModels.Admin
	var adminRole string

	err := db.QueryRow(ctx,
		`SELECT id, full_name, email, password, role, is_active FROM admins WHERE email = $1`,
		req.Email,
	).Scan(&admin.ID, &admin.FullName, &admin.Email, &admin.Password, &adminRole, &admin.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		utils.Logger.Errorf("admin login db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !admin.IsActive {
		utils.WriteError(w, "your admin account has been deactivated", http.StatusForbidden)
		return
	}

	parts := strings.Split(admin.Password, ".")
	if len(parts) != 2 {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}

	salt, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}
	storedHash, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}

	hash := argon2.IDKey([]byte(req.Password), salt, 1, 64*1024, 4, 32)
	if len(hash) != len(storedHash) || subtle.ConstantTimeCompare(hash, storedHash) != 1 {
		utils.WriteError(w, "incorrect password or credentials", http.StatusForbidden)
		return
	}

	_, _ = db.Exec(ctx, `UPDATE admins SET last_login_at = NOW() WHERE id = $1`, admin.ID)
	logAudit(ctx, db, admin.ID, "admin.login", "admin", &admin.ID, nil, r)

	token, err := utils.SignToken(admin.ID, admin.FullName, adminRole)
	if err != nil {
		utils.Logger.Errorf("failed to sign admin token: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.Logger.Errorf("failed to generate admin refresh token: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := utils.StoreAdminRefreshToken(ctx, admin.ID, refreshToken, ""); err != nil {
		utils.Logger.Errorf("failed to store admin refresh token: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(15 * time.Minute),
		SameSite: http.SameSiteNoneMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/v1/admin/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		SameSite: http.SameSiteStrictMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "admin login successful",
		"token":   token,
		"admin": map[string]interface{}{
			"id":        admin.ID,
			"full_name": admin.FullName,
			"email":     admin.Email,
			"role":      adminRole,
		},
	})
}

// ============================================================================
// POST /admin/auth/logout
// ============================================================================

// AdminLogoutHandler godoc
// @Summary      Admin logout
// @Description  Clears the admin Bearer cookie.
// @Tags         Admin Auth
// @Produce      json
// @Success      200  {object}  object{message=string}
// @Router       /admin/auth/logout [post]
// @Security     BearerAuth
func AdminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteNoneMode,
	})

	utils.WriteJSON(w, map[string]string{"message": "logged out successfully"})

	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		hash := utils.HashRefreshToken(cookie.Value)
		go utils.RevokeAdminRefreshToken(context.Background(), hash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/v1/admin/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteStrictMode,
	})
}

// ============================================================================
// GET /admin/auth/me
// ============================================================================

// AdminGetMeHandler godoc
// @Summary      Get current admin
// @Description  Returns the profile of the currently authenticated admin.
// @Tags         Admin Auth
// @Produce      json
// @Success      200  {object}  object{status=string,data=adminModels.Admin}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/auth/me [get]
// @Security     BearerAuth
func AdminGetMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin", "support"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	adminID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var admin adminModels.Admin
	err := db.QueryRow(ctx,
		`SELECT id, full_name, email, role, is_active, last_login_at, created_at FROM admins WHERE id = $1`,
		adminID,
	).Scan(&admin.ID, &admin.FullName, &admin.Email, &admin.Role, &admin.IsActive, &admin.LastLoginAt, &admin.CreatedAt)
	if err != nil {
		utils.WriteError(w, "admin not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "data": admin})
}

// ============================================================================
// PATCH /admin/auth/password
// ============================================================================

// AdminUpdatePasswordHandler godoc
// @Summary      Update admin password
// @Description  Updates the authenticated admin's password. Requires the current password.
// @Tags         Admin Auth
// @Accept       json
// @Produce      json
// @Param        body  body  adminModels.UpdateAdminPasswordRequest  true  "Password update payload"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /admin/auth/password [patch]
// @Security     BearerAuth
func AdminUpdatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin", "support"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	adminID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req adminModels.UpdateAdminPasswordRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.CurrentPassword == "" || req.NewPassword == "" {
		utils.WriteError(w, "current_password and new_password are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var storedHash string
	if err := db.QueryRow(ctx, `SELECT password FROM admins WHERE id = $1`, adminID).Scan(&storedHash); err != nil {
		utils.WriteError(w, "admin not found", http.StatusNotFound)
		return
	}

	if err := utils.VerifyPassword(req.CurrentPassword, storedHash); err != nil {
		utils.WriteError(w, "current password is incorrect", http.StatusBadRequest)
		return
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx,
		`UPDATE admins SET password = $1, password_changed_at = NOW(), updated_at = NOW() WHERE id = $2`,
		newHash, adminID,
	)
	if err != nil {
		utils.WriteError(w, "failed to update password", http.StatusInternalServerError)
		return
	}

	logAudit(ctx, db, adminID, "admin.password_change", "admin", &adminID, nil, r)
	utils.WriteJSON(w, map[string]string{"status": "success", "message": "password updated successfully"})
}

// ============================================================================
// POST /admin/admins  (super_admin only)
// ============================================================================

// CreateAdminHandler godoc
// @Summary      Create admin
// @Description  Creates a new admin account. Only super_admin can perform this action. Role defaults to support if not provided.
// @Tags         Admin Management
// @Accept       json
// @Produce      json
// @Param        body  body  adminModels.CreateAdminRequest  true  "New admin details"
// @Success      201   {object}  object{status=string,message=string,data=object{id=string,full_name=string,email=string,role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /admin/admins [post]
// @Security     BearerAuth
func CreateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin"); err != nil {
		utils.WriteError(w, "forbidden: super_admin only", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req adminModels.CreateAdminRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)

	if req.FullName == "" || req.Email == "" || req.Password == "" {
		utils.WriteError(w, "full_name, email, and password are required", http.StatusBadRequest)
		return
	}

	if req.Role == "" {
		req.Role = adminModels.RoleSupport
	}

	validRoles := map[adminModels.AdminRole]bool{
		adminModels.RoleSuperAdmin: true,
		adminModels.RoleAdmin:      true,
		adminModels.RoleSupport:    true,
	}
	if !validRoles[req.Role] {
		utils.WriteError(w, "role must be one of: super_admin, admin, support", http.StatusBadRequest)
		return
	}

	if err := utils.ValidateEmail(req.Email); err != nil {
		utils.WriteError(w, err.Error(), http.StatusBadRequest)
		return
	}

	hashedPwd, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var newID uuid.UUID
	err = db.QueryRow(ctx,
		`INSERT INTO admins (full_name, email, password, role, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		req.FullName, req.Email, hashedPwd, req.Role, callerID,
	).Scan(&newID)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			utils.WriteError(w, "an admin with that email already exists", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("create admin db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	logAudit(ctx, db, callerID, "admin.create", "admin", &newID, map[string]interface{}{
		"email": req.Email,
		"role":  req.Role,
	}, r)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "admin created successfully",
		"data": map[string]interface{}{
			"id":        newID,
			"full_name": req.FullName,
			"email":     req.Email,
			"role":      req.Role,
		},
	})
}

// ============================================================================
// GET /admin/admins  (super_admin only)
// ============================================================================

// ListAdminsHandler godoc
// @Summary      List admins
// @Description  Returns a paginated list of all admin accounts. Super admin only.
// @Tags         Admin Management
// @Produce      json
// @Param        page      query  int  false  "Page number (default 1)"
// @Param        per_page  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  adminModels.PaginatedResponse
// @Failure      403  {object}  object{error=string}
// @Router       /admin/admins [get]
// @Security     BearerAuth
func ListAdminsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin"); err != nil {
		utils.WriteError(w, "forbidden: super_admin only", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	page, perPage := handlers.ParsePagination(r)

	var total int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&total)

	rows, err := db.Query(ctx,
		`SELECT id, full_name, email, role, is_active, last_login_at, created_at
		 FROM admins ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		perPage, (page-1)*perPage,
	)
	if err != nil {
		utils.Logger.Errorf("list admins db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var admins []adminModels.Admin
	for rows.Next() {
		var a adminModels.Admin
		if err := rows.Scan(&a.ID, &a.FullName, &a.Email, &a.Role, &a.IsActive, &a.LastLoginAt, &a.CreatedAt); err != nil {
			continue
		}
		admins = append(admins, a)
	}

	utils.WriteJSON(w, handlers.BuildPaginatedResponse(admins, total, page, perPage))
}

// ============================================================================
// PATCH /admin/admins/{id}  (super_admin only)
// ============================================================================

// UpdateAdminHandler godoc
// @Summary      Update admin
// @Description  Updates an admin's full_name, role, or is_active status. Super admin only.
// @Tags         Admin Management
// @Accept       json
// @Produce      json
// @Param        id    path  string                        true  "Admin UUID"
// @Param        body  body  adminModels.UpdateAdminRequest  true  "Fields to update"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /admin/admins/{id} [patch]
// @Security     BearerAuth
func UpdateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin"); err != nil {
		utils.WriteError(w, "forbidden: super_admin only", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	targetID, err := handlers.ParseUUIDFromPath(r, "id")
	if err != nil {
		utils.WriteError(w, "invalid admin id", http.StatusBadRequest)
		return
	}

	var req adminModels.UpdateAdminRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var exists bool
	_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, targetID).Scan(&exists)
	if !exists {
		utils.WriteError(w, "admin not found", http.StatusNotFound)
		return
	}

	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argIdx := 1

	if req.FullName != "" {
		setClauses = append(setClauses, "full_name = $"+handlers.Itoa(argIdx))
		args = append(args, req.FullName)
		argIdx++
	}
	if req.Role != "" {
		setClauses = append(setClauses, "role = $"+handlers.Itoa(argIdx))
		args = append(args, req.Role)
		argIdx++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, "is_active = $"+handlers.Itoa(argIdx))
		args = append(args, *req.IsActive)
		argIdx++
	}

	if len(args) == 0 {
		utils.WriteError(w, "no fields to update", http.StatusBadRequest)
		return
	}

	args = append(args, targetID)
	query := "UPDATE admins SET " + strings.Join(setClauses, ", ") + " WHERE id = $" + handlers.Itoa(argIdx)

	_, err = db.Exec(ctx, query, args...)
	if err != nil {
		utils.Logger.Errorf("update admin db error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	logAudit(ctx, db, callerID, "admin.update", "admin", &targetID, req, r)
	utils.WriteJSON(w, map[string]string{"status": "success", "message": "admin updated successfully"})
}

// ============================================================================
// POST /admin/device/tokens
// ============================================================================

// RegisterAdminDevice godoc
// @Summary      Register admin device
// @Description  Registers a device FCM token for an admin to receive push notifications. device_type must be android or ios.
// @Tags         Admin Devices
// @Accept       json
// @Produce      json
// @Param        body  body  object{fcm_token=string,device_type=string}  true  "Device registration payload"
// @Success      200   {object}  object{message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /admin/device/tokens [post]
// @Security     BearerAuth
func RegisterAdminDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin", "support"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	adminID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		FCMToken   string `json:"fcm_token"`
		DeviceType string `json:"device_type"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.FCMToken = strings.TrimSpace(req.FCMToken)
	req.DeviceType = strings.ToLower(strings.TrimSpace(req.DeviceType))

	if req.FCMToken == "" {
		utils.WriteError(w, "fcm_token is required", http.StatusBadRequest)
		return
	}
	if req.DeviceType == "" {
		utils.WriteError(w, "device_type is required", http.StatusBadRequest)
		return
	}
	if !handlers.AllowedDeviceTypes[req.DeviceType] {
		utils.WriteError(w, "device_type must be android or ios", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(r.Context(), `
		INSERT INTO admin_device_tokens (user_id, fcm_token, device_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (fcm_token) DO UPDATE
			SET user_id     = EXCLUDED.user_id,
			    device_type = EXCLUDED.device_type,
			    updated_at  = now()
	`, adminID, req.FCMToken, req.DeviceType)
	if err != nil {
		utils.Logger.Errorf("failed to register admin device: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]string{"message": "device registered successfully"})
}

// suppress unused import
var _ = sql.ErrNoRows
