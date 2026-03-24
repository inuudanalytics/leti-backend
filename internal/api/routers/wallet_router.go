package routers

import (
	"leti_server/internal/api/handlers/wallet"
	"net/http"
)

func walletRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /wallets/top-up", wallet.FundWallet)

	mux.HandleFunc("POST /wallets/request", wallet.RequestWithdrawal)

	mux.HandleFunc("GET /wallets/withdrawals", wallet.GetWithdrawalHistory)

	mux.HandleFunc("GET /wallets/verify/payment", wallet.VerifyPaymentCallback)

	return mux
}
