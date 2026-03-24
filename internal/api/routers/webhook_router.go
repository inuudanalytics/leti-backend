package routers

import (
	paymentwebhook "leti_server/internal/api/handlers/payment_webhook"
	"net/http"
)

func webhookRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /webhooks/paystack", paymentwebhook.PaystackWebhook)

	return mux
}
