package routers

import (
	notifications "leti_server/internal/api/handlers/notificatons"
	"net/http"
)

func notificationsRouter() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /notifications/all", notifications.GetNotifications)
	mux.HandleFunc("PATCH /notifications/read-all", notifications.MarkAllNotificationsRead)
	mux.HandleFunc("PATCH /notifications/{id}/read", notifications.MarkNotificationRead)
	mux.HandleFunc("DELETE /notifications/{id}", notifications.DeleteNotification)

	return mux
}
