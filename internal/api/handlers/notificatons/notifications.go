package notifications

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
)

// ============================================================================
// GET /notifications/all
// ============================================================================

// GetNotifications godoc
// @Summary      Get notifications
// @Description  Returns a paginated list of notifications for the authenticated user. Supports filtering by category and read status. The unread_count in the response always reflects the global unread count regardless of filters.
// @Tags         Notifications
// @Produce      json
// @Param        category  query  string  false  "Filter by category: bookings, jobs, payments, reviews, system"
// @Param        is_read   query  string  false  "Filter by read status: true or false"
// @Param        page      query  int     false  "Page number (default 1)"
// @Param        limit     query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,unread_count=int,count=int,data=[]object{id=string,type=string,title=string,body=string,data=object{},is_read=bool,created_at=string},pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Router       /notifications/all [get]
// @Security     BearerAuth
func GetNotifications(w http.ResponseWriter, r *http.Request) {
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

	// ── Category → notification types mapping ─────────────────────────────────
	categoryTypes := map[string][]string{
		"bookings": {
			string(utils.NotifBookingRequest),
			string(utils.NotifBookingConfirmed),
			string(utils.NotifBookingDeclined),
			string(utils.NotifBookingCancelled),
			string(utils.NotifBookingCompleted),
			string(utils.NotifBookingReminder),
			string(utils.NotifBookingCheckedIn),
			string(utils.NotifBookingCheckedOut),
		},
		"jobs": {
			string(utils.NotifJobRequest),
			string(utils.NotifJobAccepted),
			string(utils.NotifJobDeclined),
			string(utils.NotifJobCancelled),
			string(utils.NotifJobCompleted),
			string(utils.NotifJobQuoteReceived),
			string(utils.NotifJobQuoteAccepted),
			string(utils.NotifJobQuoteRejected),
		},
		"payments": {
			string(utils.NotifPaymentReceived),
			string(utils.NotifPaymentReleased),
			string(utils.NotifPaymentHeld),
			string(utils.NotifPaymentRefunded),
			string(utils.NotifEscrowFunded),
		},
		"reviews": {
			string(utils.NotifReviewReceived),
		},
		"system": {
			string(utils.NotifRoleActivated),
			string(utils.NotifDisputeFiled),
			string(utils.NotifDisputeResolved),
			string(utils.NotifSupportTicketOpened),
			string(utils.NotifSupportTicketReply),
			string(utils.NotifSupportTicketResolved),
			string(utils.NotifGeneral),
		},
	}

	category := r.URL.Query().Get("category")
	if category != "" {
		if _, valid := categoryTypes[category]; !valid {
			utils.WriteError(w, "invalid category: must be one of bookings, jobs, payments, reviews, system", http.StatusBadRequest)
			return
		}
	}

	isReadFilter := r.URL.Query().Get("is_read")
	if isReadFilter != "" && isReadFilter != "true" && isReadFilter != "false" {
		utils.WriteError(w, "invalid is_read value: must be true or false", http.StatusBadRequest)
		return
	}

	// ── Build WHERE clause ────────────────────────────────────────────────────
	args := []interface{}{userID}
	argIdx := 2
	where := "user_id = $1"

	if category != "" {
		where += fmt.Sprintf(" AND type = ANY($%d::notification_type[])", argIdx)
		args = append(args, categoryTypes[category])
		argIdx++
	}
	if isReadFilter != "" {
		where += fmt.Sprintf(" AND is_read = $%d", argIdx)
		args = append(args, isReadFilter == "true")
		argIdx++
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	// ── Total count (respects filters) ────────────────────────────────────────
	var totalCount int
	err := db.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT COUNT(*) FROM notifications WHERE %s`, where),
		args...,
	).Scan(&totalCount)
	if err != nil {
		utils.Logger.Errorf("failed to count notifications: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Global unread count (not affected by filters) ─────────────────────────
	var unreadCount int
	err = db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND is_read = FALSE`,
		userID,
	).Scan(&unreadCount)
	if err != nil {
		utils.Logger.Errorf("failed to count unread notifications: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Fetch ─────────────────────────────────────────────────────────────────
	fetchArgs := append(args, limit, offset)
	rows, err := db.Query(r.Context(), fmt.Sprintf(`
		SELECT id, type, title, body, data, is_read, created_at
		FROM notifications
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1), fetchArgs...)
	if err != nil {
		utils.Logger.Errorf("failed to fetch notifications: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type NotificationItem struct {
		ID        uuid.UUID              `json:"id"`
		Type      string                 `json:"type"`
		Title     string                 `json:"title"`
		Body      string                 `json:"body"`
		Data      map[string]interface{} `json:"data,omitempty"`
		IsRead    bool                   `json:"is_read"`
		CreatedAt time.Time              `json:"created_at"`
	}

	notifications := make([]NotificationItem, 0)
	for rows.Next() {
		var item NotificationItem
		var dataJSON []byte
		if err := rows.Scan(
			&item.ID, &item.Type, &item.Title, &item.Body,
			&dataJSON, &item.IsRead, &item.CreatedAt,
		); err != nil {
			utils.Logger.Errorf("failed to scan notification row: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if len(dataJSON) > 0 {
			json.Unmarshal(dataJSON, &item.Data)
		}
		notifications = append(notifications, item)
	}
	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("notification row iteration error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	totalPages := (totalCount + limit - 1) / limit

	utils.WriteJSON(w, map[string]interface{}{
		"status":       "success",
		"unread_count": unreadCount,
		"count":        len(notifications),
		"data":         notifications,
		"pagination": map[string]int{
			"total":       totalCount,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// PATCH /notifications/{id}/read
// ============================================================================

// MarkNotificationRead godoc
// @Summary      Mark notification as read
// @Description  Marks a single notification as read. The notification must belong to the authenticated user.
// @Tags         Notifications
// @Produce      json
// @Param        id   path  string  true  "Notification UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /notifications/{id}/read [patch]
// @Security     BearerAuth
func MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
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

	notifID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid notification id", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(r.Context(), `
		UPDATE notifications SET is_read = TRUE WHERE id = $1 AND user_id = $2
	`, notifID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to mark notification as read: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "notification not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "notification marked as read",
	})
}

// ============================================================================
// PATCH /notifications/read-all
// ============================================================================

// MarkAllNotificationsRead godoc
// @Summary      Mark all notifications as read
// @Description  Marks all unread notifications as read for the authenticated user.
// @Tags         Notifications
// @Produce      json
// @Success      200  {object}  object{status=string,message=string,updated=int}
// @Failure      401  {object}  object{error=string}
// @Router       /notifications/read-all [patch]
// @Security     BearerAuth
func MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
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

	result, err := db.Exec(r.Context(), `
		UPDATE notifications SET is_read = TRUE WHERE user_id = $1 AND is_read = FALSE
	`, userID)
	if err != nil {
		utils.Logger.Errorf("failed to mark all notifications as read: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "all notifications marked as read",
		"updated": result.RowsAffected(),
	})
}

// ============================================================================
// DELETE /notifications/{id}
// ============================================================================

// DeleteNotification godoc
// @Summary      Delete notification
// @Description  Permanently deletes a single notification. The notification must belong to the authenticated user.
// @Tags         Notifications
// @Produce      json
// @Param        id   path  string  true  "Notification UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /notifications/{id} [delete]
// @Security     BearerAuth
func DeleteNotification(w http.ResponseWriter, r *http.Request) {
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

	notifID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid notification id", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(r.Context(), `
		DELETE FROM notifications WHERE id = $1 AND user_id = $2
	`, notifID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to delete notification: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "notification not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "notification deleted",
	})
}
