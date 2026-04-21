package wallet

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"leti_server/internal/api/services"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ============================================================================
// POST /wallets/fund
// ============================================================================
// Initialises a Paystack checkout. The webhook handles the actual credit
// when Paystack fires charge.success with transaction_type = "credit".

// FundWallet godoc
// @Summary      Fund wallet via Paystack
// @Description  Initialises a Paystack payment to top up the authenticated user's wallet. Returns a Paystack checkout URL. The wallet is credited automatically once the webhook confirms payment.
// @Tags         Wallet
// @Accept       json
// @Produce      json
// @Param        body  body  object{amount=number,email=string}  true  "amount in Naira (min 100); email only required if account has none"
// @Success      200   {object}  object{status=bool,message=string,data=object{authorization_url=string,reference=string}}
// @Failure      400   {object}  object{error=string}
// @Router       /wallets/fund [post]
// @Security     BearerAuth
func FundWallet(w http.ResponseWriter, r *http.Request) {
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

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		Amount decimal.Decimal `json:"amount"`
		Email  string          `json:"email,omitempty"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		utils.WriteError(w, "amount must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Amount.LessThan(decimal.NewFromInt(100)) {
		utils.WriteError(w, "minimum top-up amount is ₦100", http.StatusBadRequest)
		return
	}

	var dbEmail sql.NullString
	err := db.QueryRow(r.Context(),
		`SELECT email FROM users WHERE id = $1`, userID,
	).Scan(&dbEmail)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch user: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var paystackEmail string
	if dbEmail.Valid && dbEmail.String != "" {
		paystackEmail = dbEmail.String
	} else if req.Email != "" {
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
		paystackEmail = req.Email
	} else {
		utils.WriteError(w, "your account has no email address — please provide one to proceed with payment", http.StatusBadRequest)
		return
	}

	ps, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	amountKobo := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()
	form := map[string]interface{}{
		"email":  paystackEmail,
		"amount": amountKobo,
		"metadata": map[string]interface{}{
			"user_id":          userID.String(),
			"transaction_type": "credit",
		},
	}

	res, err := ps.InitializePayment(form)
	if err != nil {
		utils.Logger.Errorf("wallet fund init failed for user %s: %v", userID, err)
		utils.WriteError(w, fmt.Sprintf("failed to initialize payment: %v", err), http.StatusBadRequest)
		return
	}

	utils.WriteJSON(w, res)
}

// ============================================================================
// GET /wallets/verify/payment?reference=xxx
// ============================================================================
// Frontend callback — lets the client confirm the status of a Paystack payment
// after redirect. The webhook is the source of truth for crediting; this
// endpoint is purely informational.

// VerifyPaymentCallback godoc
// @Summary      Verify a Paystack payment (frontend callback)
// @Description  Verifies the status of a Paystack payment by reference. Used as the redirect callback after Paystack checkout. Note: wallet crediting is handled by the webhook — this endpoint is informational only.
// @Tags         Wallet
// @Produce      json
// @Param        reference  query  string  false  "Paystack payment reference"
// @Param        trxref     query  string  false  "Paystack trxref (alias)"
// @Success      200  {object}  object{status=string,message=string,reference=string,amount=number}
// @Failure      400  {object}  object{status=string,message=string,reference=string,amount=number}
// @Router       /wallets/verify/payment [get]
func VerifyPaymentCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("DB not initialized")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	paymentRef := q.Get("reference")
	if paymentRef == "" {
		paymentRef = q.Get("trxref")
	}
	if paymentRef == "" {
		utils.WriteError(w, "payment reference is required", http.StatusBadRequest)
		return
	}

	ps, err := services.NewPaystackClient()
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := ps.VerifyPayment(paymentRef)
	if err != nil {
		utils.WriteError(w, err.Error(), http.StatusBadRequest)
		return
	}

	data, ok := response.Data.(map[string]interface{})
	if !ok {
		utils.WriteError(w, "invalid response format from Paystack", http.StatusInternalServerError)
		return
	}

	status, _ := data["status"].(string)

	var amount float64
	switch v := data["amount"].(type) {
	case float64:
		amount = v / 100
	case int64:
		amount = float64(v) / 100
	}

	if status == "success" {
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "success",
			"message":   "payment verified successfully",
			"reference": paymentRef,
			"amount":    amount,
		})
	} else {
		w.WriteHeader(http.StatusBadRequest)
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "error",
			"message":   "payment was not successful",
			"reference": paymentRef,
			"amount":    amount,
		})
	}
}
