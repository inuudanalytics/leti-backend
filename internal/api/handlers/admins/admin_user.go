package admins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/api/services"
	adminModels "leti_server/internal/models/admins"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// GET /admin/users
// ============================================================================

// AdminListUsersHandler godoc
// @Summary      List users
// @Description  Returns a paginated list of all users. Supports filtering by active_role, status, and a search term that matches first_name, last_name, email, or phone_number. Soft-deleted users are excluded.
// @Tags         Admin Users
// @Produce      json
// @Param        page      query  int     false  "Page number (default 1)"
// @Param        per_page  query  int     false  "Items per page (default 20)"
// @Param        role      query  string  false  "Filter by role: client, artisan, owner"
// @Param        status    query  string  false  "Filter by status: approved, pending, suspended, probation, rejected"
// @Param        search    query  string  false  "Search by first_name, last_name, email, or phone_number"
// @Success      200  {object}  adminModels.PaginatedResponse
// @Failure      403  {object}  object{error=string}
// @Router       /admin/users [get]
// @Security     BearerAuth
func AdminListUsersHandler(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	page, perPage := handlers.ParsePagination(r)
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	conditions := []string{"u.deleted_at IS NULL"}
	args := []interface{}{}
	idx := 1

	if role != "" {
		conditions = append(conditions, "u.active_role = $"+handlers.Itoa(idx))
		args = append(args, role)
		idx++
	}
	if status != "" {
		conditions = append(conditions, "u.status = $"+handlers.Itoa(idx))
		args = append(args, status)
		idx++
	}
	if search != "" {
		conditions = append(conditions,
			"(u.first_name ILIKE $"+handlers.Itoa(idx)+
				" OR u.last_name ILIKE $"+handlers.Itoa(idx)+
				" OR u.email ILIKE $"+handlers.Itoa(idx)+
				" OR u.phone_number ILIKE $"+handlers.Itoa(idx)+")",
		)
		args = append(args, "%"+search+"%")
		idx++
	}

	where := strings.Join(conditions, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM users u WHERE "+where, countArgs...).Scan(&total); err != nil {
		utils.Logger.Errorf("admin count users error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	args = append(args, perPage, (page-1)*perPage)
	rows, err := db.Query(ctx,
		`SELECT u.id, u.first_name, u.last_name, u.email, u.phone_number,
		        u.active_role, u.status, u.email_verified, u.phone_verified,
		        u.auth_provider, u.user_created_at, u.avatar
		 FROM users u WHERE `+where+`
		 ORDER BY u.user_created_at DESC
		 LIMIT $`+handlers.Itoa(idx)+` OFFSET $`+handlers.Itoa(idx+1),
		args...,
	)
	if err != nil {
		utils.Logger.Errorf("admin list users query error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type userRow struct {
		ID            string    `json:"id"`
		FirstName     string    `json:"first_name"`
		LastName      string    `json:"last_name"`
		Email         string    `json:"email,omitempty"`
		PhoneNumber   string    `json:"phone_number,omitempty"`
		ActiveRole    string    `json:"active_role"`
		Status        string    `json:"status"`
		EmailVerified bool      `json:"email_verified"`
		PhoneVerified bool      `json:"phone_verified"`
		AuthProvider  string    `json:"auth_provider"`
		CreatedAt     time.Time `json:"created_at"`
		Avatar        string    `json:"avatar,omitempty"`
	}

	var users []userRow
	for rows.Next() {
		var u userRow
		var avatar, email, phone strings.Builder
		var avatarNull, emailNull, phoneNull *string

		if err := rows.Scan(
			&u.ID, &u.FirstName, &u.LastName, &emailNull, &phoneNull,
			&u.ActiveRole, &u.Status, &u.EmailVerified, &u.PhoneVerified,
			&u.AuthProvider, &u.CreatedAt, &avatarNull,
		); err != nil {
			utils.Logger.Errorf("admin list users scan error: %v", err)
			continue
		}
		_ = avatar
		_ = email
		_ = phone
		if emailNull != nil {
			u.Email = *emailNull
		}
		if phoneNull != nil {
			u.PhoneNumber = *phoneNull
		}
		if avatarNull != nil {
			u.Avatar = *avatarNull
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("admin list users rows error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, handlers.BuildPaginatedResponse(users, total, page, perPage))
}

// ============================================================================
// GET /admin/users/{id}
// ============================================================================

// AdminGetUserHandler godoc
// @Summary      Get user by ID
// @Description  Returns full profile of a single user including bank details where applicable. Role-specific data (artisan services, owner listings) will be added as those modules are built.
// @Tags         Admin Users
// @Produce      json
// @Param        id   path  string  true  "User UUID"
// @Success      200  {object}  object{status=string,data=object{user=object{}}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/users/{id} [get]
// @Security     BearerAuth
func AdminGetUserHandler(w http.ResponseWriter, r *http.Request) {
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

	userID := handlers.ParsePathParam(r, "id")
	if userID == "" {
		utils.WriteError(w, "user id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	type BaseUser struct {
		ID            string    `json:"id"`
		FirstName     string    `json:"first_name"`
		LastName      string    `json:"last_name"`
		Email         *string   `json:"email,omitempty"`
		PhoneNumber   *string   `json:"phone_number,omitempty"`
		Avatar        *string   `json:"avatar,omitempty"`
		ActiveRole    string    `json:"active_role"`
		Status        string    `json:"status"`
		IsOnline      *bool     `json:"is_online,omitempty"`
		PhoneVerified bool      `json:"phone_verified"`
		EmailVerified bool      `json:"email_verified"`
		AuthProvider  string    `json:"auth_provider"`
		CreatedAt     time.Time `json:"created_at"`
	}

	var u BaseUser
	var avatarBytes []byte

	err := db.QueryRow(ctx, `
		SELECT id, first_name, last_name, email, phone_number, avatar,
		       active_role, status, is_online,
		       phone_verified, email_verified, auth_provider, user_created_at
		FROM users
		WHERE id = $1::uuid AND deleted_at IS NULL
	`, userID).Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.PhoneNumber, &avatarBytes,
		&u.ActiveRole, &u.Status, &u.IsOnline,
		&u.PhoneVerified, &u.EmailVerified, &u.AuthProvider, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("admin get user error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if len(avatarBytes) > 0 {
		s := string(avatarBytes)
		u.Avatar = &s
	}

	// Fetch all roles this user has activated
	roleRows, err := db.Query(ctx, `SELECT role FROM user_roles WHERE user_id = $1::uuid ORDER BY created_at ASC`, userID)
	if err != nil {
		utils.Logger.Errorf("admin get user roles error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer roleRows.Close()

	var roles []string
	for roleRows.Next() {
		var role string
		if err := roleRows.Scan(&role); err != nil {
			continue
		}
		roles = append(roles, role)
	}

	// Fetch wallet
	type walletInfo struct {
		Balance   float64    `json:"balance"`
		Currency  string     `json:"currency"`
		IsActive  bool       `json:"is_active"`
		CreatedAt time.Time  `json:"created_at"`
		LastTxAt  *time.Time `json:"last_transaction_at,omitempty"`
	}

	var wallet walletInfo
	_ = db.QueryRow(ctx, `
		SELECT balance, currency, is_active, wallet_created_at, last_transaction_at
		FROM wallets WHERE user_id = $1::uuid
	`, userID).Scan(&wallet.Balance, &wallet.Currency, &wallet.IsActive, &wallet.CreatedAt, &wallet.LastTxAt)

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"user":   u,
			"roles":  roles,
			"wallet": wallet,
		},
	})
}

// ============================================================================
// PATCH /admin/users/{id}/status
// ============================================================================

// AdminUpdateUserStatusHandler godoc
// @Summary      Update user status
// @Description  Updates a user's account status. Valid values are approved, suspended, probation, rejected, pending.
// @Tags         Admin Users
// @Accept       json
// @Produce      json
// @Param        id    path  string                          true  "User UUID"
// @Param        body  body  adminModels.UpdateUserStatusRequest  true  "Status update"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /admin/users/{id}/status [patch]
// @Security     BearerAuth
func AdminUpdateUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
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

	userID, err := handlers.ParseUUIDFromPath(r, "id")
	if err != nil {
		utils.WriteError(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var req adminModels.UpdateUserStatusRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	allowed := map[string]bool{
		"approved": true, "suspended": true, "probation": true,
		"rejected": true, "pending": true,
	}
	if !allowed[req.Status] {
		utils.WriteError(w, "invalid status value", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var oldStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&oldStatus)

	result, err := db.Exec(ctx, `UPDATE users SET status = $1 WHERE id = $2 AND deleted_at IS NULL`, req.Status, userID)
	if err != nil {
		utils.Logger.Errorf("update user status error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "user not found", http.StatusNotFound)
		return
	}

	logAudit(ctx, db, callerID, "user.status_change", "user", &userID, map[string]interface{}{
		"old_status": oldStatus,
		"new_status": req.Status,
		"reason":     req.Reason,
	}, r)

	utils.WriteJSON(w, map[string]string{
		"status":  "success",
		"message": "user status updated to " + req.Status,
	})
}

// ============================================================================
// DELETE /admin/users/{id}
// ============================================================================

// AdminDeleteUserHandler godoc
// @Summary      Delete user
// @Description  Permanently deletes a user account. This is a hard delete — use with caution. Super admin only.
// @Tags         Admin Users
// @Produce      json
// @Param        id   path  string  true  "User UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/users/{id} [delete]
// @Security     BearerAuth
func AdminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
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

	userID, err := handlers.ParseUUIDFromPath(r, "id")
	if err != nil {
		utils.WriteError(w, "invalid user id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var email string
	_ = db.QueryRow(ctx, `SELECT COALESCE(email, phone_number, '') FROM users WHERE id = $1`, userID).Scan(&email)

	result, err := db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		utils.Logger.Errorf("delete user error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "user not found", http.StatusNotFound)
		return
	}

	logAudit(ctx, db, callerID, "user.delete", "user", &userID, map[string]interface{}{"email": email}, r)
	utils.WriteJSON(w, map[string]string{"status": "success", "message": "user deleted successfully"})
}

// ============================================================================
// GET /admin/withdrawals
// ============================================================================

// AdminListWithdrawalsHandler godoc
// @Summary      List withdrawals
// @Description  Returns a paginated list of all withdrawals. Optionally filter by status: pending, processing, successful, failed.
// @Tags         Admin Finance
// @Produce      json
// @Param        page      query  int     false  "Page number"
// @Param        per_page  query  int     false  "Items per page"
// @Param        status    query  string  false  "Filter by status: pending, processing, successful, failed"
// @Success      200  {object}  object{status=string,count=int,data=[]object{},pagination=object{}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/withdrawals [get]
// @Security     BearerAuth
func AdminListWithdrawalsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	validStatuses := map[string]bool{
		"pending": true, "processing": true, "successful": true, "failed": true,
	}

	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" && !validStatuses[statusFilter] {
		utils.WriteError(w, "invalid status. must be one of: pending, processing, successful, failed", http.StatusBadRequest)
		return
	}

	page, perPage := handlers.ParsePagination(r)
	offset := (page - 1) * perPage

	where := "1=1"
	args := []interface{}{}
	argIdx := 1

	if statusFilter != "" {
		where += fmt.Sprintf(" AND wd.status = $%d", argIdx)
		args = append(args, statusFilter)
		argIdx++
	}

	var total int
	_ = db.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT COUNT(*) FROM withdrawals wd WHERE %s`, where),
		args...,
	).Scan(&total)

	args = append(args, perPage, offset)
	rows, err := db.Query(r.Context(), fmt.Sprintf(`
		SELECT wd.id, wd.user_id,
		       u.first_name, u.last_name, u.active_role,
		       wd.amount, wd.fee, wd.net_amount, wd.status,
		       wd.bank_detail_id, wd.failure_reason,
		       wd.transfer_reference, wd.transfer_code,
		       wd.initiated_at, wd.completed_at
		FROM withdrawals wd
		JOIN users u ON u.id = wd.user_id
		WHERE %s
		ORDER BY wd.initiated_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1), args...)
	if err != nil {
		utils.Logger.Errorf("failed to list withdrawals: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type WithdrawalRow struct {
		ID                uuid.UUID  `json:"id"`
		UserID            uuid.UUID  `json:"user_id"`
		FirstName         string     `json:"first_name"`
		LastName          string     `json:"last_name"`
		ActiveRole        string     `json:"active_role"`
		Amount            float64    `json:"amount"`
		Fee               float64    `json:"fee"`
		NetAmount         float64    `json:"net_amount"`
		Status            string     `json:"status"`
		BankDetailID      uuid.UUID  `json:"bank_detail_id"`
		FailureReason     *string    `json:"failure_reason,omitempty"`
		TransferReference *string    `json:"transfer_reference,omitempty"`
		TransferCode      *string    `json:"transfer_code,omitempty"`
		InitiatedAt       time.Time  `json:"initiated_at"`
		CompletedAt       *time.Time `json:"completed_at,omitempty"`
	}

	list := make([]WithdrawalRow, 0)
	for rows.Next() {
		var item WithdrawalRow
		if err := rows.Scan(
			&item.ID, &item.UserID,
			&item.FirstName, &item.LastName, &item.ActiveRole,
			&item.Amount, &item.Fee, &item.NetAmount, &item.Status,
			&item.BankDetailID, &item.FailureReason,
			&item.TransferReference, &item.TransferCode,
			&item.InitiatedAt, &item.CompletedAt,
		); err != nil {
			utils.Logger.Errorf("failed to scan withdrawal row: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		list = append(list, item)
	}

	totalPages := (total + perPage - 1) / perPage
	utils.WriteJSON(w, map[string]interface{}{
		"status":        "success",
		"count":         len(list),
		"active_filter": statusFilter,
		"data":          list,
		"pagination": map[string]int{
			"total": total, "page": page, "limit": perPage, "total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /admin/withdrawals/{id}/verify
// ============================================================================

// AdminVerifyWithdrawalTransferHandler godoc
// @Summary      Verify withdrawal transfer
// @Description  Checks the live transfer status directly from Paystack for a given withdrawal. Automatically syncs the DB status if the webhook was missed.
// @Tags         Admin Finance
// @Produce      json
// @Param        id   path  string  true  "Withdrawal UUID"
// @Success      200  {object}  object{status=string,data=object{withdrawal_id=string,our_status=string,paystack_status=string,transfer_ref=string}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/withdrawals/{id}/verify [get]
// @Security     BearerAuth
func AdminVerifyWithdrawalTransferHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	withdrawalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid withdrawal id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var transferRef, currentStatus string
	err = db.QueryRow(ctx,
		`SELECT transfer_reference, status FROM withdrawals WHERE id = $1`,
		withdrawalID,
	).Scan(&transferRef, &currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "withdrawal not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if transferRef == "" {
		utils.WriteError(w, "no transfer reference found — this withdrawal has not been approved yet", http.StatusBadRequest)
		return
	}

	paystackClient, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, "payment service unavailable", http.StatusInternalServerError)
		return
	}

	res, err := paystackClient.VerifyTransfer(transferRef)
	if err != nil {
		utils.Logger.Errorf("failed to verify transfer for withdrawal %s: %v", withdrawalID, err)
		utils.WriteError(w, "could not verify transfer with Paystack", http.StatusBadGateway)
		return
	}

	transferData, _ := res.Data.(map[string]interface{})
	paystackStatus, _ := transferData["status"].(string)

	if currentStatus == "processing" {
		switch paystackStatus {
		case "success":
			_, _ = db.Exec(ctx, `
				UPDATE withdrawals SET status = 'successful', completed_at = NOW()
				WHERE id = $1 AND status = 'processing'
			`, withdrawalID)
			var userID uuid.UUID
			if scanErr := db.QueryRow(ctx, `SELECT user_id FROM withdrawals WHERE id = $1`, withdrawalID).Scan(&userID); scanErr == nil {
				go utils.CreateNotification(context.Background(), userID, utils.NotifPaymentReleased,
					"Withdrawal Successful",
					"Your withdrawal has been sent to your bank account successfully.",
					nil,
				)
			}
		case "failed", "reversed":
			var userID, walletID uuid.UUID
			var amount, fee float64
			scanErr := db.QueryRow(ctx,
				`SELECT user_id, wallet_id, amount, fee FROM withdrawals WHERE id = $1`,
				withdrawalID,
			).Scan(&userID, &walletID, &amount, &fee)
			if scanErr == nil {
				_, _ = db.Exec(ctx, `
					UPDATE withdrawals SET status = 'failed',
					    failure_reason = 'Transfer failed or reversed. Funds refunded.', completed_at = NOW()
					WHERE id = $1 AND status = 'processing'
				`, withdrawalID)
				_, _ = db.Exec(ctx, `
					UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE id = $2
				`, amount+fee, walletID)
				go utils.CreateNotification(context.Background(), userID, utils.NotifPaymentRefunded,
					"Withdrawal Failed",
					"Your withdrawal could not be processed and has been refunded to your wallet.",
					nil,
				)
			}
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"withdrawal_id":    withdrawalID,
			"our_status":       currentStatus,
			"paystack_status":  paystackStatus,
			"transfer_ref":     transferRef,
			"paystack_details": transferData,
		},
	})
}
