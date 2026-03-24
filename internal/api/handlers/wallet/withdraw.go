package wallet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/api/services"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================================
// POST /wallet/withdraw
// ============================================================================

// RequestWithdrawal godoc
// @Summary      Withdraw funds from wallet
// @Description  Initiates a direct Paystack transfer to the user's primary bank account. A ₦50 processing fee is deducted. The wallet is debited immediately; funds arrive once Paystack confirms the transfer. Requires a primary bank account to be set up first.
// @Tags         Wallet
// @Accept       json
// @Produce      json
// @Param        body  body  object{amount=number}  true  "Amount in Naira (min ₦100)"
// @Success      200   {object}  object{status=string,message=string,data=object{withdrawal_id=string,amount=number,fee=number,bank_name=string,account_number=string,status=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /wallet/withdraw [post]
// @Security     BearerAuth
func RequestWithdrawal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("DB not initialized")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "client" && role != "artisan" && role != "owner" {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		Amount float64 `json:"amount"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Amount <= 0 {
		utils.WriteError(w, "amount must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Amount < 100 {
		utils.WriteError(w, "minimum withdrawal amount is ₦100", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var walletID uuid.UUID
	var balance float64
	err := db.QueryRow(ctx, `
		SELECT id, balance FROM wallets
		WHERE user_id = $1 AND is_active = TRUE
	`, userID).Scan(&walletID, &balance)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "wallet not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch wallet: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	const withdrawalFee = 50.0
	totalDeduction := req.Amount + withdrawalFee

	if balance < totalDeduction {
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "error",
			"message":   fmt.Sprintf("insufficient balance. You need ₦%.2f (₦%.2f + ₦%.2f fee) but have ₦%.2f", totalDeduction, req.Amount, withdrawalFee, balance),
			"code":      "INSUFFICIENT_BALANCE",
			"required":  totalDeduction,
			"available": balance,
		})
		return
	}

	var processingExists bool
	err = db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM withdrawals
			WHERE user_id = $1 AND status IN ('pending', 'processing')
		)
	`, userID).Scan(&processingExists)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if processingExists {
		utils.WriteError(w, "you already have a withdrawal in progress", http.StatusConflict)
		return
	}

	type bankDetail struct {
		ID            uuid.UUID
		BankName      string
		BankCode      string
		AccountNumber string
		AccountName   string
		RecipientCode string
	}
	var bank bankDetail

	switch role {
	case "client":
		err = db.QueryRow(ctx, `
			SELECT id, bank_name, bank_code, account_number, account_name,
			       COALESCE(recipient_code, '')
			FROM client_bank_details
			WHERE client_id = $1 AND is_primary = TRUE
		`, userID).Scan(
			&bank.ID, &bank.BankName, &bank.BankCode,
			&bank.AccountNumber, &bank.AccountName, &bank.RecipientCode,
		)
	case "artisan":
		err = db.QueryRow(ctx, `
			SELECT id, bank_name, bank_code, account_number, account_name,
			       COALESCE(recipient_code, '')
			FROM artisan_bank_details
			WHERE artisan_id = $1 AND is_primary = TRUE
		`, userID).Scan(
			&bank.ID, &bank.BankName, &bank.BankCode,
			&bank.AccountNumber, &bank.AccountName, &bank.RecipientCode,
		)
	case "owner":
		err = db.QueryRow(ctx, `
			SELECT id, bank_name, bank_code, account_number, account_name,
			       COALESCE(recipient_code, '')
			FROM owner_bank_details
			WHERE owner_id = $1 AND is_primary = TRUE
		`, userID).Scan(
			&bank.ID, &bank.BankName, &bank.BankCode,
			&bank.AccountNumber, &bank.AccountName, &bank.RecipientCode,
		)
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "no primary bank account found — please add and set a primary bank account before withdrawing", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("failed to fetch bank details for %s %s: %v", role, userID, err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if bank.RecipientCode == "" {
		utils.WriteError(w, "your bank account is not fully set up for transfers — please re-add your bank details", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance - $1, last_transaction_at = NOW()
		WHERE id = $2
	`, totalDeduction, walletID)
	if err != nil {
		utils.Logger.Errorf("failed to debit wallet: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var withdrawalID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO withdrawals (
			user_id, wallet_id, bank_detail_id,
			amount, fee, net_amount, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'processing')
		RETURNING id
	`, userID, walletID, bank.ID, req.Amount, withdrawalFee, req.Amount).Scan(&withdrawalID)
	if err != nil {
		utils.Logger.Errorf("failed to create withdrawal record: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	transferRef := fmt.Sprintf("WDR-%s-%d", withdrawalID.String()[:8], time.Now().UnixMilli())
	_, err = tx.Exec(ctx, `
		UPDATE withdrawals SET transfer_reference = $1 WHERE id = $2
	`, transferRef, withdrawalID)
	if err != nil {
		utils.Logger.Errorf("failed to store transfer reference: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
		VALUES ($1, $2, 'withdrawal', $3)
	`, walletID, totalDeduction, withdrawalID)
	if err != nil {
		utils.Logger.Errorf("failed to record wallet transaction: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.Logger.Errorf("failed to commit withdrawal: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ps, err := services.NewPaystackClient()
	if err != nil {
		refundWithdrawal(ctx, db, withdrawalID, walletID, totalDeduction, "Payment service unavailable.")
		utils.WriteError(w, "payment service unavailable — your funds have been refunded", http.StatusInternalServerError)
		return
	}

	amountKobo := int64(req.Amount * 100)
	transferRes, err := ps.InitiateTransfer(map[string]interface{}{
		"source":    "balance",
		"amount":    amountKobo,
		"recipient": bank.RecipientCode,
		"reason":    "Leti wallet withdrawal",
		"reference": transferRef,
	})
	if err != nil {
		utils.Logger.Errorf("Paystack transfer failed for withdrawal %s: %v", withdrawalID, err)
		refundWithdrawal(ctx, db, withdrawalID, walletID, totalDeduction,
			"Transfer could not be initiated. Please try again or contact support.")

		go utils.CreateNotification(
			context.Background(), userID, utils.NotifGeneral,
			"Withdrawal Failed",
			"Your withdrawal could not be processed and has been refunded to your wallet.",
			nil,
		)

		utils.WriteError(w, "withdrawal failed — your funds have been refunded to your wallet", http.StatusBadGateway)
		return
	}

	if transferData, ok := transferRes.Data.(map[string]interface{}); ok {
		if transferCode, ok := transferData["transfer_code"].(string); ok && transferCode != "" {
			_, _ = db.Exec(ctx, `
				UPDATE withdrawals SET transfer_code = $1 WHERE id = $2
			`, transferCode, withdrawalID)
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("withdrawal of ₦%.2f is being processed to your %s account", req.Amount, bank.BankName),
		"data": map[string]interface{}{
			"withdrawal_id":  withdrawalID,
			"amount":         req.Amount,
			"fee":            withdrawalFee,
			"net_amount":     req.Amount,
			"bank_name":      bank.BankName,
			"account_number": bank.AccountNumber,
			"account_name":   bank.AccountName,
			"status":         "processing",
		},
	})
}

// ============================================================================
// GET /wallet/withdrawals
// ============================================================================

// GetWithdrawalHistory godoc
// @Summary      Get withdrawal history
// @Description  Returns a paginated list of the authenticated user's past withdrawals.
// @Tags         Wallet
// @Produce      json
// @Param        page   query  int  false  "Page (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]object,pagination=object}
// @Router       /wallet/withdrawals [get]
// @Security     BearerAuth
func GetWithdrawalHistory(w http.ResponseWriter, r *http.Request) {
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

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	var total int
	_ = db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM withdrawals WHERE user_id = $1
	`, userID).Scan(&total)

	rows, err := db.Query(r.Context(), `
		SELECT id, amount, fee, net_amount, status,
		       failure_reason, initiated_at, completed_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY initiated_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("failed to fetch withdrawals: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type WithdrawalItem struct {
		ID            uuid.UUID  `json:"id"`
		Amount        float64    `json:"amount"`
		Fee           float64    `json:"fee"`
		NetAmount     float64    `json:"net_amount"`
		Status        string     `json:"status"`
		FailureReason *string    `json:"failure_reason,omitempty"`
		InitiatedAt   time.Time  `json:"initiated_at"`
		CompletedAt   *time.Time `json:"completed_at,omitempty"`
	}

	withdrawals := make([]WithdrawalItem, 0)
	for rows.Next() {
		var item WithdrawalItem
		if err := rows.Scan(
			&item.ID, &item.Amount, &item.Fee, &item.NetAmount,
			&item.Status, &item.FailureReason,
			&item.InitiatedAt, &item.CompletedAt,
		); err != nil {
			utils.Logger.Errorf("failed to scan withdrawal: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		withdrawals = append(withdrawals, item)
	}

	totalPages := (total + limit - 1) / limit

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(withdrawals),
		"data":   withdrawals,
		"pagination": map[string]int{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// refundWithdrawal — marks withdrawal failed and returns funds to wallet.
// Called on Paystack errors before a transfer is sent.
// ============================================================================
func refundWithdrawal(
	ctx context.Context,
	db *pgxpool.Pool,
	withdrawalID, walletID uuid.UUID,
	totalDeduction float64,
	reason string,
) {
	_, _ = db.Exec(ctx, `
		UPDATE withdrawals
		SET status = 'failed', failure_reason = $1, completed_at = NOW()
		WHERE id = $2
	`, reason, withdrawalID)

	_, _ = db.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $1, last_transaction_at = NOW()
		WHERE id = $2
	`, totalDeduction, walletID)
}
