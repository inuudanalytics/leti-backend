package routers

import (
	shortletchathandler "leti_server/internal/api/handlers/shortlet_chat"
	"net/http"
)

func shortletChatRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /shortlet-chat/ws",
		shortletchathandler.ServeWS)

	mux.HandleFunc("POST /shortlet-chat/conversations",
		shortletchathandler.StartShortletConversation)

	mux.HandleFunc("GET /shortlet-chat/conversations",
		shortletchathandler.GetMyShortletConversations)

	mux.HandleFunc("GET /shortlet-chat/conversations/{id}/messages",
		shortletchathandler.GetShortletMessages)

	mux.HandleFunc("PATCH /shortlet-chat/conversations/{id}/read",
		shortletchathandler.MarkShortletMessagesRead)

	mux.HandleFunc("POST /shortlet-chat/conversations/{id}/images",
		shortletchathandler.SendShortletImageMessage)

	mux.HandleFunc("DELETE /shortlet-chat/conversations/{id}",
		shortletchathandler.DeleteShortletConversation)

	return mux
}
