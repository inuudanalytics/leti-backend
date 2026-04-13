package routers

import (
	"leti_server/internal/api/handlers/calls"
	"net/http"
)

func callRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("POST /calls/start", http.HandlerFunc(calls.StartCall))
	mux.Handle("POST /calls/token", http.HandlerFunc(calls.GetCallToken))
	mux.Handle("POST /calls/end", http.HandlerFunc(calls.EndCall))
	mux.Handle("GET  /calls/history", http.HandlerFunc(calls.GetCallHistory))
	mux.Handle("POST /calls/{call_id}/accept", http.HandlerFunc(calls.AcceptCall))
	mux.Handle("POST /calls/{call_id}/reject", http.HandlerFunc(calls.RejectCall))
	mux.Handle("GET  /calls/{call_id}", http.HandlerFunc(calls.GetCall))

	return mux
}
