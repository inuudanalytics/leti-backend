package routers

import (
	"leti_server/internal/api/handlers/chat"
	"net/http"
)

func chatRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /chat/ws", chat.ServeWS)

	mux.HandleFunc("POST /chat/conversations/create", chat.StartConversation)

	mux.HandleFunc("GET /chat/conversations", chat.GetMyConversations)

	mux.HandleFunc("GET /chat/conversations/{id}/messages", chat.GetMessages)

	mux.HandleFunc("PATCH /chat/conversations/{id}/read", chat.MarkMessagesRead)

	mux.HandleFunc("POST /chat/conversations/images/upload/{id}", chat.SendImageMessage)

	mux.HandleFunc("DELETE /chat/conversations/{id}", chat.DeleteConversation)

	return mux
}
