package routers

import (
	"leti_server/internal/api/handlers/support"
	"net/http"
)

func supportRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /support/ws", support.ServeUserSupportWS)

	// Ticket
	mux.HandleFunc("POST /support/tickets", support.CreateSupportTicket)
	mux.HandleFunc("GET /support/tickets", support.GetMyTickets)
	mux.HandleFunc("GET /support/tickets/{id}", support.GetMyTicket)

	// Messaging
	mux.HandleFunc("GET /support/tickets/{id}/messages", support.GetTicketMessages)
	mux.HandleFunc("POST /support/tickets/{id}/messages/images", support.SendTicketImageMessage)
	mux.HandleFunc("PATCH /support/tickets/{id}/read", support.MarkTicketMessagesRead)

	return mux
}
