package support

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	supportModels "leti_server/internal/models/support"
	"leti_server/internal/repositories/sqlconnect"
	supporthub "leti_server/internal/supporthub"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

var Hub *supporthub.Hub

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// validCategories
var validCategories = map[string]bool{
	"payment_and_refund":       true,
	"booking_and_reservation":  true,
	"service_issues":           true,
	"disputes":                 true,
	"account_and_verification": true,
	"technical_issue":          true,
	"report_user_or_property":  true,
	"general_inquiry":          true,
	"other":                    true,
}

var validPriorities = map[string]bool{
	"low": true, "medium": true, "high": true, "urgent": true,
}

// ============================================================================
// WS  GET /api/v1/support/ws
// ============================================================================

// ServeUserSupportWS godoc
// @Summary      Connect to live support chat (WebSocket)
// @Description  Upgrades the connection to a WebSocket for real-time support chat. Clients, artisans, and owners can all connect. Send JSON frames with type "message", "read", or "typing".
// @Description
// @Description  **Outbound frame types:** `message`, `read`, `typing`, `error`
// @Description
// @Description  **Inbound frame shape:**
// @Description  ```json
// @Description  { "type": "message", "ticket_id": "<uuid>", "content": "hello" }
// @Description  { "type": "read",    "ticket_id": "<uuid>" }
// @Description  { "type": "typing",  "ticket_id": "<uuid>" }
// @Description  ```
// @Tags         Support
// @Success      101  {string}  string  "Switching Protocols"
// @Failure      401  {object}  object{error=string}
// @Router       /support/ws [get]
// @Security     BearerAuth
func ServeUserSupportWS(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.Logger.Errorf("support ws upgrade failed for user %s: %v", userID, err)
		return
	}

	client := Hub.NewClient(conn, userID, "user")
	go client.WritePump()
	go client.ReadPump()
}

// ============================================================================
// POST /api/v1/support/tickets
// ============================================================================

// CreateSupportTicket godoc
// @Summary      Create a support ticket
// @Description  Any authenticated platform user (client, artisan, or owner) can open a support ticket. Each ticket gets a unique ID and starts in "open" status.
// @Tags         Support
// @Accept       json
// @Produce      json
// @Param        body  body  object{subject=string,category=string,priority=string}  true  "Ticket details. category must be one of: payment_and_refund, booking_and_reservation, service_issues, disputes, account_and_verification, technical_issue, report_user_or_property, general_inquiry, other. priority: low | medium | high | urgent (default: medium)."
// @Success      201   {object}  object{status=string,message=string,data=object{id=string,user_id=string,subject=string,category=string,status=string,priority=string,created_at=string,updated_at=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      500   {object}  object{error=string}
// @Router       /support/tickets [post]
// @Security     BearerAuth
func CreateSupportTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	type request struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Priority string `json:"priority"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		utils.WriteError(w, "subject is required", http.StatusBadRequest)
		return
	}
	if len(req.Subject) > 255 {
		utils.WriteError(w, "subject must be at most 255 characters", http.StatusBadRequest)
		return
	}

	if req.Category == "" {
		req.Category = "general_inquiry"
	}
	if !validCategories[req.Category] {
		utils.WriteError(w,
			"invalid category. must be one of: payment_and_refund, booking_and_reservation, service_issues, disputes, account_and_verification, technical_issue, report_user_or_property, general_inquiry, other",
			http.StatusBadRequest,
		)
		return
	}

	if req.Priority == "" {
		req.Priority = "medium"
	}
	if !validPriorities[req.Priority] {
		utils.WriteError(w, "invalid priority. must be one of: low, medium, high, urgent", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ticket supportModels.SupportTicket
	err := db.QueryRow(ctx, `
		INSERT INTO support_tickets (user_id, subject, category, priority)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, assigned_admin_id,
		          subject, category, status, priority, resolved_at, created_at, updated_at
	`, userID, req.Subject, req.Category, req.Priority).Scan(
		&ticket.ID, &ticket.UserID, &ticket.AssignedAdminID,
		&ticket.Subject, &ticket.Category,
		&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
		&ticket.CreatedAt, &ticket.UpdatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("failed to create support ticket: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(context.Background(), userID, utils.NotifSupportTicketOpened,
		"Support Ticket Created",
		fmt.Sprintf("Your support ticket '%s' has been opened. We'll respond soon.", req.Subject),
		map[string]interface{}{"ticket_id": ticket.ID},
	)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "support ticket created",
		"data":    ticket,
	})
}

// ============================================================================
// GET /api/v1/support/tickets
// ============================================================================

// GetMyTickets godoc
// @Summary      List my support tickets
// @Description  Returns all support tickets belonging to the authenticated user (client, artisan, or owner), paginated newest-first. Also returns unread admin message count per ticket.
// @Tags         Support
// @Produce      json
// @Param        page   query  int  false  "Page number (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=array,pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      401  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /support/tickets [get]
// @Security     BearerAuth
func GetMyTickets(w http.ResponseWriter, r *http.Request) {
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
	db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM support_tickets WHERE user_id = $1
	`, userID).Scan(&total)

	rows, err := db.Query(r.Context(), `
		SELECT
			t.id, t.user_id, t.assigned_admin_id,
			t.subject, t.category, t.status, t.priority,
			t.resolved_at, t.created_at, t.updated_at,
			(
				SELECT COUNT(*)
				FROM support_messages sm
				WHERE sm.ticket_id = t.id
				  AND sm.sender_type = 'admin'
				  AND sm.is_read = FALSE
			) AS unread_count
		FROM support_tickets t
		WHERE t.user_id = $1
		ORDER BY t.updated_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TicketRow struct {
		supportModels.SupportTicket
		UnreadCount int `json:"unread_count"`
	}

	tickets := make([]TicketRow, 0)
	for rows.Next() {
		var t TicketRow
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.AssignedAdminID,
			&t.Subject, &t.Category, &t.Status, &t.Priority,
			&t.ResolvedAt, &t.CreatedAt, &t.UpdatedAt,
			&t.UnreadCount,
		); err != nil {
			continue
		}
		tickets = append(tickets, t)
	}

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(tickets),
		"data":   tickets,
		"pagination": map[string]int{
			"total": total, "page": page,
			"limit": limit, "total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /api/v1/support/tickets/{id}
// ============================================================================

// GetMyTicket godoc
// @Summary      Get a single support ticket
// @Description  Returns the full detail of one support ticket. The authenticated user must be the ticket owner.
// @Tags         Support
// @Produce      json
// @Param        id   path  string  true  "Ticket UUID"
// @Success      200  {object}  object{status=string,data=object}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /support/tickets/{id} [get]
// @Security     BearerAuth
func GetMyTicket(w http.ResponseWriter, r *http.Request) {
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

	ticketID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var ticket supportModels.SupportTicket
	err = db.QueryRow(r.Context(), `
		SELECT id, user_id, assigned_admin_id,
		       subject, category, status, priority, resolved_at, created_at, updated_at
		FROM support_tickets
		WHERE id = $1
	`, ticketID).Scan(
		&ticket.ID, &ticket.UserID, &ticket.AssignedAdminID,
		&ticket.Subject, &ticket.Category,
		&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
		&ticket.CreatedAt, &ticket.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Only the ticket owner may fetch it
	if ticket.UserID != userID {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "data": ticket})
}

// ============================================================================
// GET /api/v1/support/tickets/{id}/messages
// ============================================================================

// GetTicketMessages godoc
// @Summary      Get ticket message history
// @Description  Returns paginated messages for a ticket oldest-first. Also marks all unread admin messages as read. The authenticated user must own the ticket.
// @Tags         Support
// @Produce      json
// @Param        id      path   string  true   "Ticket UUID"
// @Param        limit   query  int     false  "Max messages to return (1–100, default 50)"
// @Param        offset  query  int     false  "Offset for pagination (default 0)"
// @Success      200  {object}  object{status=string,count=int,data=array,pagination=object{total=int,limit=int,offset=int}}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /support/tickets/{id}/messages [get]
// @Security     BearerAuth
func GetTicketMessages(w http.ResponseWriter, r *http.Request) {
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

	ticketID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var ownerID uuid.UUID
	err = db.QueryRow(r.Context(), `
		SELECT user_id FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ownerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if ownerID != userID {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 0 {
			offset = n
		}
	}

	var total int
	db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM support_messages WHERE ticket_id = $1
	`, ticketID).Scan(&total)

	rows, err := db.Query(r.Context(), `
		SELECT id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
		FROM support_messages
		WHERE ticket_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
	`, ticketID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]supportModels.SupportMessage, 0)
	for rows.Next() {
		var m supportModels.SupportMessage
		if err := rows.Scan(
			&m.ID, &m.TicketID, &m.SenderID, &m.SenderType,
			&m.Content, &m.MsgType, &m.IsRead, &m.CreatedAt,
		); err != nil {
			continue
		}
		messages = append(messages, m)
	}

	db.Exec(r.Context(), `
		UPDATE support_messages
		SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_type = 'admin' AND is_read = FALSE
	`, ticketID)

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(messages),
		"data":   messages,
		"pagination": map[string]int{
			"total": total, "limit": limit, "offset": offset,
		},
	})
}

// ============================================================================
// POST /api/v1/support/tickets/{id}/messages/images
// ============================================================================

// SendTicketImageMessage godoc
// @Summary      Send image message(s) in a support ticket
// @Description  Uploads 1–5 images to Cloudinary and posts them as image-type messages in the ticket. The authenticated user must own the ticket and it must not be closed or resolved.
// @Tags         Support
// @Accept       multipart/form-data
// @Produce      json
// @Param        id      path      string  true   "Ticket UUID"
// @Param        images  formData  file    true   "Image file(s) — up to 5"
// @Success      201  {object}  object{status=string,messages=array}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /support/tickets/{id}/messages/images [post]
// @Security     BearerAuth
func SendTicketImageMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	ticketID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var ticketUserID uuid.UUID
	var assignedAdminID *uuid.UUID
	var ticketStatus string
	err = db.QueryRow(r.Context(), `
		SELECT user_id, assigned_admin_id, status FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ticketUserID, &assignedAdminID, &ticketStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if ticketUserID != userID {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}
	if ticketStatus == "closed" || ticketStatus == "resolved" {
		utils.WriteError(w, "ticket is closed", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	imageHeaders := r.MultipartForm.File["images"]
	if len(imageHeaders) == 0 {
		utils.WriteError(w, "at least one image is required", http.StatusBadRequest)
		return
	}
	if len(imageHeaders) > 5 {
		utils.WriteError(w, "maximum 5 images allowed", http.StatusBadRequest)
		return
	}

	var files []utils.UploadFile
	var openFiles []io.Closer
	for _, h := range imageHeaders {
		f, err := h.Open()
		if err != nil {
			continue
		}
		files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
		openFiles = append(openFiles, f)
	}
	defer func() {
		for _, f := range openFiles {
			f.Close()
		}
	}()

	cloud, err := utils.InitCloudinary()
	if err != nil {
		utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
		return
	}

	urls, err := cloud.UploadImages(ctx, files, "support/images", 0)
	if err != nil {
		utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
		return
	}

	var savedMessages []supportModels.SupportMessage
	for _, url := range urls {
		var msg supportModels.SupportMessage
		err = db.QueryRow(r.Context(), `
			INSERT INTO support_messages (ticket_id, sender_id, sender_type, content, msg_type)
			VALUES ($1, $2, 'user', $3, 'image')
			RETURNING id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
		`, ticketID, userID, url).Scan(
			&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderType,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		)
		if err != nil {
			utils.Logger.Errorf("failed to insert image message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		payload := buildSupportWSPayload("message", msg)
		if Hub != nil {
			Hub.DeliverTo(userID, payload)
			if assignedAdminID != nil {
				Hub.DeliverTo(*assignedAdminID, payload)
			}
		}
		savedMessages = append(savedMessages, msg)
	}

	if assignedAdminID != nil {
		go handlers.SendPushToUser(*assignedAdminID, "Support Image", "User sent an image in a support ticket")
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"messages": savedMessages,
	})
}

// ============================================================================
// PATCH /api/v1/support/tickets/{id}/read
// ============================================================================

// MarkTicketMessagesRead godoc
// @Summary      Mark admin messages as read
// @Description  Marks all unread admin messages in the ticket as read. The authenticated user must own the ticket.
// @Tags         Support
// @Produce      json
// @Param        id  path  string  true  "Ticket UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /support/tickets/{id}/read [patch]
// @Security     BearerAuth
func MarkTicketMessagesRead(w http.ResponseWriter, r *http.Request) {
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

	ticketID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var ownerID uuid.UUID
	err = db.QueryRow(r.Context(), `
		SELECT user_id FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ownerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if ownerID != userID {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db.Exec(r.Context(), `
		UPDATE support_messages
		SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_type = 'admin' AND is_read = FALSE
	`, ticketID)

	utils.WriteJSON(w, map[string]string{
		"status":  "success",
		"message": "messages marked as read",
	})
}

// ============================================================================
// Helpers
// ============================================================================

func buildSupportWSPayload(msgType string, payload interface{}) []byte {
	type outgoing struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}
	b, _ := json.Marshal(outgoing{Type: msgType, Payload: payload})
	return b
}
