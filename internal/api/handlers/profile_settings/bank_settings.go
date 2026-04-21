package profilesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"leti_server/internal/api/services"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// Shared types
// ============================================================================

type BankDetail struct {
	ID            uuid.UUID `json:"id"`
	OwnerID       uuid.UUID `json:"owner_id"`
	BankName      string    `json:"bank_name"`
	BankCode      string    `json:"bank_code"`
	AccountNumber string    `json:"account_number"`
	AccountName   string    `json:"account_name"`
	RecipientCode string    `json:"recipient_code"`
	IsPrimary     bool      `json:"is_primary"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// bankConfig holds the table name and column name for a role's bank table.
type bankConfig struct {
	table    string // e.g. "artisan_bank_details"
	ownerCol string // e.g. "artisan_id"
	role     string // e.g. "artisan"
}

var bankConfigs = map[string]bankConfig{
	"artisan": {table: "artisan_bank_details", ownerCol: "artisan_id", role: "artisan"},
	"client":  {table: "client_bank_details", ownerCol: "client_id", role: "client"},
	"owner":   {table: "owner_bank_details", ownerCol: "owner_id", role: "owner"},
}

// ============================================================================
// POST /profile/bank/verify  — verify bank details via Paystack (all roles)
// ============================================================================

// VerifyBankDetails godoc
// @Summary      Verify bank account
// @Description  Verifies a bank account number against a bank via Paystack. Available to artisan, client, and owner roles.
// @Tags         Bank Details
// @Accept       json
// @Produce      json
// @Param        body  body  object{account_number=string,bank_name=string}  true  "Bank verification payload"
// @Success      200   {object}  object{status=string,message=string,data=object}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /profile/bank/verify [post]
// @Security     BearerAuth
func VerifyBankDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		AccountNumber string `json:"account_number"`
		BankName      string `json:"bank_name"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.AccountNumber == "" {
		utils.WriteError(w, "account_number is required", http.StatusBadRequest)
		return
	}
	if req.BankName == "" {
		utils.WriteError(w, "bank_name is required", http.StatusBadRequest)
		return
	}

	paystackClient, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	res, err := paystackClient.VerifyBankDetails(req.AccountNumber, req.BankName)
	if err != nil {
		utils.WriteError(w, fmt.Sprintf("bank verification failed: %v", err), http.StatusBadRequest)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "bank details verified",
		"data":    res.Data,
	})
}

// ============================================================================
// GET /profile/bank/list  — list all supported banks from Paystack (all roles)
// ============================================================================

// GetBankList godoc
// @Summary      Get supported bank list
// @Description  Returns a list of all banks supported by Paystack (name and code only). Available to all roles.
// @Tags         Bank Details
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]object{name=string,code=string}}
// @Failure      500  {object}  object{error=string}
// @Router       /profile/bank/list [get]
// @Security     BearerAuth
func GetBankList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	paystackClient, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	res, err := paystackClient.GetBanks()
	if err != nil {
		utils.Logger.Errorf("failed to fetch bank list: %v", err)
		utils.WriteError(w, "failed to fetch bank list", http.StatusInternalServerError)
		return
	}

	bankList, ok := res.Data.([]interface{})
	if !ok {
		utils.WriteError(w, "unexpected response from payment provider", http.StatusInternalServerError)
		return
	}

	type Bank struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}

	banks := make([]Bank, 0, len(bankList))
	for _, b := range bankList {
		bankMap, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := bankMap["name"].(string)
		code, _ := bankMap["code"].(string)
		if name == "" || code == "" {
			continue
		}
		banks = append(banks, Bank{Name: name, Code: code})
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(banks),
		"data":   banks,
	})
}

// ============================================================================
// POST /profile/artisan/bank
// ============================================================================

// SaveArtisanBankDetails godoc
// @Summary      Add artisan bank account
// @Description  Adds a bank account for the authenticated artisan. The first account is automatically set as primary. All subsequent accounts must share the same account name as the primary (to prevent identity fraud). A Paystack transfer recipient is created for each account.
// @Tags         Artisan Bank Details
// @Accept       json
// @Produce      json
// @Param        body  body  object{bank_name=string,bank_code=string,account_number=string,account_name=string}  true  "Bank account payload"
// @Success      201   {object}  object{status=string,message=string,data=BankDetail}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/artisan/bank [post]
// @Security     BearerAuth
func SaveArtisanBankDetails(w http.ResponseWriter, r *http.Request) {
	saveBankDetails(w, r, bankConfigs["artisan"])
}

// ============================================================================
// GET /profile/artisan/bank
// ============================================================================

// GetArtisanBankDetails godoc
// @Summary      Get artisan bank accounts
// @Description  Returns all bank accounts for the authenticated artisan, primary account first.
// @Tags         Artisan Bank Details
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]BankDetail}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/artisan/bank [get]
// @Security     BearerAuth
func GetArtisanBankDetails(w http.ResponseWriter, r *http.Request) {
	getBankDetails(w, r, bankConfigs["artisan"])
}

// ============================================================================
// PATCH /profile/artisan/bank/{id}/primary
// ============================================================================

// SetArtisanPrimaryBankAccount godoc
// @Summary      Set artisan primary bank account
// @Description  Marks a specific bank account as the artisan's primary payout account. The target account name must match the current primary account name.
// @Tags         Artisan Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/artisan/bank/{id}/primary [patch]
// @Security     BearerAuth
func SetArtisanPrimaryBankAccount(w http.ResponseWriter, r *http.Request) {
	setPrimaryBankAccount(w, r, bankConfigs["artisan"])
}

// ============================================================================
// DELETE /profile/artisan/bank/{id}
// ============================================================================

// DeleteArtisanBankDetails godoc
// @Summary      Delete artisan bank account
// @Description  Deletes a bank account. Cannot delete the only account or the primary account (set another as primary first).
// @Tags         Artisan Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/artisan/bank/{id} [delete]
// @Security     BearerAuth
func DeleteArtisanBankDetails(w http.ResponseWriter, r *http.Request) {
	deleteBankDetails(w, r, bankConfigs["artisan"])
}

// ============================================================================
// POST /profile/client/bank
// ============================================================================

// SaveClientBankDetails godoc
// @Summary      Add client bank account
// @Description  Adds a bank account for the authenticated client. The first account is automatically set as primary. All subsequent accounts must share the same account name as the primary. A Paystack transfer recipient is created for each account.
// @Tags         Client Bank Details
// @Accept       json
// @Produce      json
// @Param        body  body  object{bank_name=string,bank_code=string,account_number=string,account_name=string}  true  "Bank account payload"
// @Success      201   {object}  object{status=string,message=string,data=BankDetail}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/client/bank [post]
// @Security     BearerAuth
func SaveClientBankDetails(w http.ResponseWriter, r *http.Request) {
	saveBankDetails(w, r, bankConfigs["client"])
}

// ============================================================================
// GET /profile/client/bank
// ============================================================================

// GetClientBankDetails godoc
// @Summary      Get client bank accounts
// @Description  Returns all bank accounts for the authenticated client, primary account first.
// @Tags         Client Bank Details
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]BankDetail}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/client/bank [get]
// @Security     BearerAuth
func GetClientBankDetails(w http.ResponseWriter, r *http.Request) {
	getBankDetails(w, r, bankConfigs["client"])
}

// ============================================================================
// PATCH /profile/client/bank/{id}/primary
// ============================================================================

// SetClientPrimaryBankAccount godoc
// @Summary      Set client primary bank account
// @Description  Marks a specific bank account as the client's primary payout account. The target account name must match the current primary account name.
// @Tags         Client Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/client/bank/{id}/primary [patch]
// @Security     BearerAuth
func SetClientPrimaryBankAccount(w http.ResponseWriter, r *http.Request) {
	setPrimaryBankAccount(w, r, bankConfigs["client"])
}

// ============================================================================
// DELETE /profile/client/bank/{id}
// ============================================================================

// DeleteClientBankDetails godoc
// @Summary      Delete client bank account
// @Description  Deletes a bank account. Cannot delete the only account or the primary account (set another as primary first).
// @Tags         Client Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/client/bank/{id} [delete]
// @Security     BearerAuth
func DeleteClientBankDetails(w http.ResponseWriter, r *http.Request) {
	deleteBankDetails(w, r, bankConfigs["client"])
}

// ============================================================================
// POST /profile/owner/bank
// ============================================================================

// SaveOwnerBankDetails godoc
// @Summary      Add owner bank account
// @Description  Adds a bank account for the authenticated owner. The first account is automatically set as primary. All subsequent accounts must share the same account name as the primary. A Paystack transfer recipient is created for each account.
// @Tags         Owner Bank Details
// @Accept       json
// @Produce      json
// @Param        body  body  object{bank_name=string,bank_code=string,account_number=string,account_name=string}  true  "Bank account payload"
// @Success      201   {object}  object{status=string,message=string,data=BankDetail}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /profile/owner/bank [post]
// @Security     BearerAuth
func SaveOwnerBankDetails(w http.ResponseWriter, r *http.Request) {
	saveBankDetails(w, r, bankConfigs["owner"])
}

// ============================================================================
// GET /profile/owner/bank
// ============================================================================

// GetOwnerBankDetails godoc
// @Summary      Get owner bank accounts
// @Description  Returns all bank accounts for the authenticated owner, primary account first.
// @Tags         Owner Bank Details
// @Produce      json
// @Success      200  {object}  object{status=string,count=int,data=[]BankDetail}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /profile/owner/bank [get]
// @Security     BearerAuth
func GetOwnerBankDetails(w http.ResponseWriter, r *http.Request) {
	getBankDetails(w, r, bankConfigs["owner"])
}

// ============================================================================
// PATCH /profile/owner/bank/{id}/primary
// ============================================================================

// SetOwnerPrimaryBankAccount godoc
// @Summary      Set owner primary bank account
// @Description  Marks a specific bank account as the owner's primary payout account. The target account name must match the current primary account name.
// @Tags         Owner Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/owner/bank/{id}/primary [patch]
// @Security     BearerAuth
func SetOwnerPrimaryBankAccount(w http.ResponseWriter, r *http.Request) {
	setPrimaryBankAccount(w, r, bankConfigs["owner"])
}

// ============================================================================
// DELETE /profile/owner/bank/{id}
// ============================================================================

// DeleteOwnerBankDetails godoc
// @Summary      Delete owner bank account
// @Description  Deletes a bank account. Cannot delete the only account or the primary account (set another as primary first).
// @Tags         Owner Bank Details
// @Produce      json
// @Param        id  path  string  true  "Bank detail UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /profile/owner/bank/{id} [delete]
// @Security     BearerAuth
func DeleteOwnerBankDetails(w http.ResponseWriter, r *http.Request) {
	deleteBankDetails(w, r, bankConfigs["owner"])
}

// ============================================================================
// Internal shared implementations
// ============================================================================

func saveBankDetails(w http.ResponseWriter, r *http.Request, cfg bankConfig) {
	if r.Method != http.MethodPost {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can save bank details", cfg.role), http.StatusForbidden)
		return
	}

	type request struct {
		BankName      string `json:"bank_name"`
		BankCode      string `json:"bank_code"`
		AccountNumber string `json:"account_number"`
		AccountName   string `json:"account_name"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.BankName == "" {
		utils.WriteError(w, "bank_name is required", http.StatusBadRequest)
		return
	}
	if req.BankCode == "" {
		utils.WriteError(w, "bank_code is required", http.StatusBadRequest)
		return
	}
	if req.AccountNumber == "" {
		utils.WriteError(w, "account_number is required", http.StatusBadRequest)
		return
	}
	if req.AccountName == "" {
		utils.WriteError(w, "account_name is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Duplicate check
	var alreadyExists bool
	_ = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1 FROM %s
			WHERE %s = $1 AND account_number = $2 AND bank_name = $3
		)
	`, cfg.table, cfg.ownerCol), userID, req.AccountNumber, req.BankName).Scan(&alreadyExists)
	if alreadyExists {
		utils.WriteError(w, "this account has already been added", http.StatusConflict)
		return
	}

	// Enforce account name consistency with existing primary
	var primaryAccountName string
	err := db.QueryRow(ctx, fmt.Sprintf(`
		SELECT account_name FROM %s
		WHERE %s = $1 AND is_primary = TRUE
		LIMIT 1
	`, cfg.table, cfg.ownerCol), userID).Scan(&primaryAccountName)
	if err != nil && err != pgx.ErrNoRows {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if primaryAccountName != "" {
		if !strings.EqualFold(strings.TrimSpace(primaryAccountName), strings.TrimSpace(req.AccountName)) {
			utils.WriteError(w,
				fmt.Sprintf("account name does not match your existing primary account name (%s)", primaryAccountName),
				http.StatusConflict,
			)
			return
		}
	}

	// First account becomes primary automatically
	var existingCount int
	_ = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM %s WHERE %s = $1
	`, cfg.table, cfg.ownerCol), userID).Scan(&existingCount)
	isPrimary := existingCount == 0

	// Create Paystack transfer recipient
	paystackClient, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	recipientRes, err := paystackClient.CreateRecipient(map[string]interface{}{
		"type":           "nuban",
		"name":           req.AccountName,
		"account_number": req.AccountNumber,
		"bank_code":      req.BankCode,
		"currency":       "NGN",
	})
	if err != nil {
		utils.Logger.Errorf("failed to create Paystack recipient: %v", err)
		utils.WriteError(w, "failed to register bank with payment provider", http.StatusBadRequest)
		return
	}

	recipientCode := ""
	if recipientData, ok := recipientRes.Data.(map[string]interface{}); ok {
		recipientCode, _ = recipientData["recipient_code"].(string)
	}

	var saved BankDetail
	err = db.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO %s
			(%s, bank_name, bank_code, account_number, account_name, recipient_code, is_primary)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, %s, bank_name, bank_code, account_number, account_name, recipient_code, is_primary, created_at, updated_at
	`, cfg.table, cfg.ownerCol, cfg.ownerCol),
		userID, req.BankName, req.BankCode, req.AccountNumber, req.AccountName, recipientCode, isPrimary,
	).Scan(
		&saved.ID, &saved.OwnerID, &saved.BankName, &saved.BankCode,
		&saved.AccountNumber, &saved.AccountName, &saved.RecipientCode,
		&saved.IsPrimary, &saved.CreatedAt, &saved.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to save bank details (%s): %v", cfg.table, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "bank details saved successfully",
		"data":    saved,
	})
}

func getBankDetails(w http.ResponseWriter, r *http.Request, cfg bankConfig) {
	if r.Method != http.MethodGet {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can view their bank details", cfg.role), http.StatusForbidden)
		return
	}

	rows, err := db.Query(r.Context(), fmt.Sprintf(`
		SELECT id, %s, bank_name, bank_code, account_number, account_name, recipient_code, is_primary, created_at, updated_at
		FROM %s
		WHERE %s = $1
		ORDER BY is_primary DESC, created_at ASC
	`, cfg.ownerCol, cfg.table, cfg.ownerCol), userID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch bank details (%s): %v", cfg.table, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	details := make([]BankDetail, 0)
	for rows.Next() {
		var d BankDetail
		if err := rows.Scan(
			&d.ID, &d.OwnerID, &d.BankName, &d.BankCode,
			&d.AccountNumber, &d.AccountName, &d.RecipientCode,
			&d.IsPrimary, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		details = append(details, d)
	}
	if err := rows.Err(); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(details),
		"data":   details,
	})
}

func setPrimaryBankAccount(w http.ResponseWriter, r *http.Request, cfg bankConfig) {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can update bank details", cfg.role), http.StatusForbidden)
		return
	}

	bankID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid bank detail id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch target account name
	var targetAccountName string
	err = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT account_name FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), bankID, userID).Scan(&targetAccountName)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "bank details not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Enforce name match with current primary
	var currentPrimaryName string
	err = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT account_name FROM %s WHERE %s = $1 AND is_primary = TRUE LIMIT 1
	`, cfg.table, cfg.ownerCol), userID).Scan(&currentPrimaryName)
	if err != nil && err != pgx.ErrNoRows {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if currentPrimaryName != "" &&
		!strings.EqualFold(strings.TrimSpace(currentPrimaryName), strings.TrimSpace(targetAccountName)) {
		utils.WriteError(w,
			fmt.Sprintf("account name does not match your current primary account name (%s)", currentPrimaryName),
			http.StatusConflict,
		)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET is_primary = FALSE WHERE %s = $1
	`, cfg.table, cfg.ownerCol), userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET is_primary = TRUE, updated_at = NOW() WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), bankID, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "primary bank account updated",
	})
}

func deleteBankDetails(w http.ResponseWriter, r *http.Request, cfg bankConfig) {
	if r.Method != http.MethodDelete {
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
	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != cfg.role {
		utils.WriteError(w, fmt.Sprintf("only %ss can delete bank details", cfg.role), http.StatusForbidden)
		return
	}

	bankID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid bank detail id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var isPrimary bool
	err = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT is_primary FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), bankID, userID).Scan(&isPrimary)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "bank details not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var totalCount int
	_ = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM %s WHERE %s = $1
	`, cfg.table, cfg.ownerCol), userID).Scan(&totalCount)

	if totalCount == 1 {
		utils.WriteError(w,
			"you cannot delete your only bank account — add another account first",
			http.StatusConflict,
		)
		return
	}
	if isPrimary {
		utils.WriteError(w,
			"you cannot delete your primary bank account — set another account as primary first",
			http.StatusConflict,
		)
		return
	}

	_, err = db.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s WHERE id = $1 AND %s = $2
	`, cfg.table, cfg.ownerCol), bankID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to delete bank details (%s): %v", cfg.table, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "bank details deleted successfully",
	})
}
