package admins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

var SupportHub *supporthub.Hub

var adminWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ============================================================================
// WS  GET /api/v1/admin/support/ws
// ============================================================================

// ServeAdminSupportWS godoc
// @Summary      Connect to live support chat as admin (WebSocket)
// @Description  Upgrades the connection to a WebSocket for real-time admin support chat. Accessible by super_admin, admin, and support roles.
// @Description
// @Description  **Inbound frame shape:**
// @Description  ```json
// @Description  { "type": "message", "ticket_id": "<uuid>", "content": "hello" }
// @Description  { "type": "read",    "ticket_id": "<uuid>" }
// @Description  { "type": "typing",  "ticket_id": "<uuid>" }
// @Description  ```
// @Description
// @Description  **Outbound frame types:** `message`, `read`, `typing`, `error`
// @Tags         Admin Support
// @Success      101  {string}  string  "Switching Protocols"
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/support/ws [get]
// @Security     BearerAuth
func ServeAdminSupportWS(w http.ResponseWriter, r *http.Request) {
	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := adminWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.Logger.Errorf("admin support ws upgrade failed: %v", err)
		return
	}

	client := SupportHub.NewClient(conn, callerID, "admin")
	go client.WritePump()
	go client.ReadPump()
}

// ============================================================================
// GET /api/v1/admin/support/tickets
// Query params: status, category, priority, page, limit
// ============================================================================

// AdminListSupportTickets godoc
// @Summary      List all support tickets (admin)
// @Description  Returns a paginated list of all support tickets across all users, sorted by priority (urgent → high → medium → low) then by most recently updated. Includes per-ticket unread and message counts, plus global open/in-progress/resolved stats. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Produce      json
// @Param        status    query  string  false  "Filter by status: open | assigned | in_progress | waiting_user | resolved | closed"
// @Param        category  query  string  false  "Filter by category: payment_and_refund | booking_and_reservation | service_issues | disputes | account_and_verification | technical_issue | report_user_or_property | general_inquiry | other"
// @Param        priority  query  string  false  "Filter by priority: low | medium | high | urgent"
// @Param        page      query  int     false  "Page number (default 1)"
// @Param        limit     query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,stats=object{open=int,in_progress=int,resolved=int},count=int,data=array,pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      403  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets [get]
// @Security     BearerAuth
func AdminListSupportTickets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	categoryFilter := r.URL.Query().Get("category")
	priorityFilter := r.URL.Query().Get("priority")

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	args := []interface{}{}
	where := "1=1"
	idx := 1

	if statusFilter != "" {
		where += fmt.Sprintf(" AND t.status = $%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	if categoryFilter != "" {
		where += fmt.Sprintf(" AND t.category = $%d", idx)
		args = append(args, categoryFilter)
		idx++
	}
	if priorityFilter != "" {
		where += fmt.Sprintf(" AND t.priority = $%d", idx)
		args = append(args, priorityFilter)
		idx++
	}

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM support_tickets t WHERE %s`, where), countArgs...).Scan(&total)

	args = append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT
			t.id,
			t.user_id,
			u.user_name,
			t.assigned_admin_id,
			t.subject,
			t.category,
			t.status,
			t.priority,
			t.resolved_at,
			t.created_at,
			t.updated_at,
			(
				SELECT COUNT(*)
				FROM support_messages sm
				WHERE sm.ticket_id = t.id
				  AND sm.sender_type = 'user'
				  AND sm.is_read = FALSE
			) AS unread_count,
			(
				SELECT COUNT(*)
				FROM support_messages sm
				WHERE sm.ticket_id = t.id
			) AS message_count
		FROM support_tickets t
		JOIN users u ON u.id = t.user_id
		WHERE %s
		ORDER BY
			CASE t.priority
				WHEN 'urgent' THEN 1
				WHEN 'high'   THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low'    THEN 4
				ELSE 5
			END,
			t.updated_at DESC
		LIMIT $%d OFFSET $%d
	`, where, idx, idx+1), args...)
	if err != nil {
		utils.Logger.Errorf("admin list support tickets: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TicketRow struct {
		supportModels.SupportTicket
		UserName     string `json:"user_name"`
		UnreadCount  int    `json:"unread_count"`
		MessageCount int    `json:"message_count"`
	}

	tickets := make([]TicketRow, 0)
	for rows.Next() {
		var t TicketRow
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.UserName, &t.AssignedAdminID,
			&t.Subject, &t.Category, &t.Status, &t.Priority,
			&t.ResolvedAt, &t.CreatedAt, &t.UpdatedAt,
			&t.UnreadCount, &t.MessageCount,
		); err != nil {
			continue
		}
		tickets = append(tickets, t)
	}

	// Summary stats
	var openCount, inProgressCount, resolvedCount int
	db.QueryRow(ctx, `SELECT COUNT(*) FROM support_tickets WHERE status = 'open'`).Scan(&openCount)
	db.QueryRow(ctx, `SELECT COUNT(*) FROM support_tickets WHERE status IN ('assigned','in_progress','waiting_user')`).Scan(&inProgressCount)
	db.QueryRow(ctx, `SELECT COUNT(*) FROM support_tickets WHERE status IN ('resolved','closed')`).Scan(&resolvedCount)

	totalPages := (total + limit - 1) / limit
	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"stats": map[string]int{
			"open":        openCount,
			"in_progress": inProgressCount,
			"resolved":    resolvedCount,
		},
		"count": len(tickets),
		"data":  tickets,
		"pagination": map[string]int{
			"total": total, "page": page,
			"limit": limit, "total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /api/v1/admin/support/tickets/{ticketId}
// ============================================================================

// AdminGetSupportTicket godoc
// @Summary      Get a single support ticket with messages (admin)
// @Description  Returns full ticket detail including user name, email, assigned admin name, message count, and a paginated message history (oldest-first). Also marks all unread user messages as read. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Produce      json
// @Param        ticketId  path   string  true   "Ticket UUID"
// @Param        limit     query  int     false  "Max messages to return (1–100, default 50)"
// @Param        offset    query  int     false  "Message offset (default 0)"
// @Success      200  {object}  object{status=string,data=object,messages=array}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId} [get]
// @Security     BearerAuth
func AdminGetSupportTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	type ticketDetail struct {
		supportModels.SupportTicket
		UserName     string  `json:"user_name"`
		UserEmail    *string `json:"user_email,omitempty"`
		AdminName    *string `json:"assigned_admin_name,omitempty"`
		MessageCount int     `json:"message_count"`
	}

	var ticket ticketDetail
	err = db.QueryRow(ctx, `
		SELECT
			t.id,
			t.user_id,
			u.user_name,
			u.email,
			t.assigned_admin_id,
			a.full_name,
			t.subject,
			t.category,
			t.status,
			t.priority,
			t.resolved_at,
			t.created_at,
			t.updated_at,
			(SELECT COUNT(*) FROM support_messages sm WHERE sm.ticket_id = t.id)
		FROM support_tickets t
		JOIN users u ON u.id = t.user_id
		LEFT JOIN admins a ON a.id = t.assigned_admin_id
		WHERE t.id = $1
	`, ticketID).Scan(
		&ticket.ID, &ticket.UserID, &ticket.UserName, &ticket.UserEmail,
		&ticket.AssignedAdminID, &ticket.AdminName,
		&ticket.Subject, &ticket.Category, &ticket.Status, &ticket.Priority,
		&ticket.ResolvedAt, &ticket.CreatedAt, &ticket.UpdatedAt,
		&ticket.MessageCount,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("admin get support ticket: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
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

	msgRows, err := db.Query(ctx, `
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
	defer msgRows.Close()

	messages := make([]supportModels.SupportMessage, 0)
	for msgRows.Next() {
		var m supportModels.SupportMessage
		if err := msgRows.Scan(
			&m.ID, &m.TicketID, &m.SenderID, &m.SenderType,
			&m.Content, &m.MsgType, &m.IsRead, &m.CreatedAt,
		); err != nil {
			continue
		}
		messages = append(messages, m)
	}

	db.Exec(ctx, `
		UPDATE support_messages
		SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_type = 'user' AND is_read = FALSE
	`, ticketID)

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"data":     ticket,
		"messages": messages,
	})
}

// ============================================================================
// POST /api/v1/admin/support/tickets/{ticketId}/reply  — text reply
// ============================================================================

// AdminReplySupportTicket godoc
// @Summary      Send a text reply to a support ticket (admin)
// @Description  Saves a text message from an admin into the ticket, auto-assigns the ticket to the replying admin if not yet assigned, advances status from open/assigned → in_progress, delivers the message over WebSocket to both the user and the admin, and sends a push notification + in-app notification to the user. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Accept       json
// @Produce      json
// @Param        ticketId  path   string                   true  "Ticket UUID"
// @Param        body      body   object{content=string}   true  "Message content"
// @Success      200  {object}  object{status=string,message=string,data=object}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId}/reply [post]
// @Security     BearerAuth
func AdminReplySupportTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	type request struct {
		Content string `json:"content"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Content == "" {
		utils.WriteError(w, "content is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ticketUserID uuid.UUID
	var assignedAdminID *uuid.UUID
	var ticketStatus string
	err = db.QueryRow(ctx, `
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

	if ticketStatus == "closed" {
		utils.WriteError(w, "ticket is closed", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	if assignedAdminID == nil {
		_, err = tx.Exec(ctx, `
			UPDATE support_tickets
			SET assigned_admin_id = $1,
			    status            = 'in_progress',
			    updated_at        = NOW()
			WHERE id = $2
		`, callerID, ticketID)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE support_tickets
			SET status     = CASE WHEN status IN ('open','assigned') THEN 'in_progress' ELSE status END,
			    updated_at = NOW()
			WHERE id = $1
		`, ticketID)
	}
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var msg supportModels.SupportMessage
	err = tx.QueryRow(ctx, `
		INSERT INTO support_messages (ticket_id, sender_id, sender_type, content, msg_type)
		VALUES ($1, $2, 'admin', $3, 'text')
		RETURNING id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
	`, ticketID, callerID, req.Content).Scan(
		&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderType,
		&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
	)
	if err != nil {
		utils.Logger.Errorf("admin reply support ticket: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if SupportHub != nil {
		payload := buildAdminWSPayload("message", msg)
		SupportHub.DeliverTo(ticketUserID, payload)
		SupportHub.DeliverTo(callerID, payload)
	}

	go utils.CreateNotification(context.Background(), ticketUserID, utils.NotifSupportTicketReply,
		"Support Reply",
		fmt.Sprintf("An admin replied to your ticket: %s", req.Content),
		map[string]interface{}{"ticket_id": ticketID},
	)
	go handlers.SendPushToUser(ticketUserID, "Support Reply", req.Content,
		map[string]string{"screen": "SupportChat", "ticket_id": ticketID.String()})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "reply sent",
		"data":    msg,
	})
}

// ============================================================================
// POST /api/v1/admin/support/tickets/{ticketId}/reply/images  — image reply
// ============================================================================

// AdminReplySupportTicketImage godoc
// @Summary      Send image reply to a support ticket (admin)
// @Description  Uploads 1–5 images to Cloudinary and inserts them as image-type messages in the ticket. Delivers each message over WebSocket to the user and the admin, then sends a push notification to the user. The ticket must not be closed. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Accept       multipart/form-data
// @Produce      json
// @Param        ticketId  path      string  true   "Ticket UUID"
// @Param        images    formData  file    true   "Image file(s) — up to 5"
// @Success      200  {object}  object{status=string,messages=array}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId}/reply/images [post]
// @Security     BearerAuth
func AdminReplySupportTicketImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	var ticketUserID uuid.UUID
	var ticketStatus string
	err = db.QueryRow(r.Context(), `
		SELECT user_id, status FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ticketUserID, &ticketStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if ticketStatus == "closed" {
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

	urls, err := cloud.UploadImages(ctx, files, "support/admin-images", 0)
	if err != nil {
		utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
		return
	}

	var savedMessages []supportModels.SupportMessage
	for _, url := range urls {
		var msg supportModels.SupportMessage
		err = db.QueryRow(r.Context(), `
			INSERT INTO support_messages (ticket_id, sender_id, sender_type, content, msg_type)
			VALUES ($1, $2, 'admin', $3, 'image')
			RETURNING id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
		`, ticketID, callerID, url).Scan(
			&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderType,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		)
		if err != nil {
			utils.Logger.Errorf("failed to insert admin image message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if SupportHub != nil {
			payload := buildAdminWSPayload("message", msg)
			SupportHub.DeliverTo(ticketUserID, payload)
			SupportHub.DeliverTo(callerID, payload)
		}
		savedMessages = append(savedMessages, msg)
	}

	go handlers.SendPushToUser(ticketUserID, "Support Image", "Admin sent an image in your support ticket",
		map[string]string{"screen": "SupportChat", "ticket_id": ticketID.String()})

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"messages": savedMessages,
	})
}

// ============================================================================
// PATCH /api/v1/admin/support/tickets/{ticketId}/status
// ============================================================================

// AdminUpdateTicketStatus godoc
// @Summary      Update support ticket status (admin)
// @Description  Sets the ticket status to the given value. When resolving or closing, also stamps resolved_at and fires an in-app notification + push notification to the user. The action is recorded in the admin audit log. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Accept       json
// @Produce      json
// @Param        ticketId  path   string                  true  "Ticket UUID"
// @Param        body      body   object{status=string}   true  "New status. Must be one of: open | assigned | in_progress | waiting_user | resolved | closed"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId}/status [patch]
// @Security     BearerAuth
func AdminUpdateTicketStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	type request struct {
		Status string `json:"status"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	validStatuses := map[string]bool{
		"open": true, "assigned": true, "in_progress": true,
		"waiting_user": true, "resolved": true, "closed": true,
	}
	if !validStatuses[req.Status] {
		utils.WriteError(w,
			"invalid status. must be one of: open, assigned, in_progress, waiting_user, resolved, closed",
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ticketUserID uuid.UUID
	err = db.QueryRow(ctx, `SELECT user_id FROM support_tickets WHERE id = $1`, ticketID).Scan(&ticketUserID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "ticket not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	isClosing := req.Status == "resolved" || req.Status == "closed"

	var execErr error
	if isClosing {
		_, execErr = db.Exec(ctx, `
			UPDATE support_tickets
			SET status = $1, resolved_at = NOW(), updated_at = NOW()
			WHERE id = $2
		`, req.Status, ticketID)
	} else {
		_, execErr = db.Exec(ctx, `
			UPDATE support_tickets
			SET status = $1, updated_at = NOW()
			WHERE id = $2
		`, req.Status, ticketID)
	}
	if execErr != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	logAudit(ctx, db, callerID, "support.status_update", "support_ticket", &ticketID,
		map[string]interface{}{"new_status": req.Status}, r)

	if isClosing {
		go utils.CreateNotification(context.Background(), ticketUserID, utils.NotifSupportTicketResolved,
			"Support Ticket Resolved",
			"Your support ticket has been resolved.",
			map[string]interface{}{"ticket_id": ticketID},
		)
		go handlers.SendPushToUser(ticketUserID, "Ticket Resolved", "Your support ticket has been resolved.",
			map[string]string{"screen": "Support", "ticket_id": ticketID.String()})
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("ticket status updated to %s", req.Status),
	})
}

// ============================================================================
// PATCH /api/v1/admin/support/tickets/{ticketId}/assign
// ============================================================================

// AdminAssignTicket godoc
// @Summary      Assign a support ticket to an admin (admin)
// @Description  Sets assigned_admin_id on the ticket and moves status to 'assigned'. Restricted to super_admin and admin roles only (support agents cannot reassign tickets).
// @Tags         Admin Support
// @Accept       json
// @Produce      json
// @Param        ticketId  path   string                    true  "Ticket UUID"
// @Param        body      body   object{admin_id=string}   true  "UUID of the admin to assign"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId}/assign [patch]
// @Security     BearerAuth
func AdminAssignTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	type request struct {
		AdminID string `json:"admin_id"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	adminID, err := uuid.Parse(req.AdminID)
	if err != nil {
		utils.WriteError(w, "invalid admin_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := db.Exec(ctx, `
		UPDATE support_tickets
		SET assigned_admin_id = $1,
		    status            = 'assigned',
		    updated_at        = NOW()
		WHERE id = $2
	`, adminID, ticketID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "ticket not found", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "ticket assigned",
	})
}

// ============================================================================
// GET /api/v1/admin/support/tickets/{ticketId}/messages
// ============================================================================

// AdminGetTicketMessages godoc
// @Summary      Get message history for a support ticket (admin)
// @Description  Returns paginated messages for a ticket oldest-first (up to 200 per page). Also marks all unread user messages in the ticket as read. Accessible by super_admin, admin, and support roles.
// @Tags         Admin Support
// @Produce      json
// @Param        ticketId  path   string  true   "Ticket UUID"
// @Param        limit     query  int     false  "Max messages to return (1–200, default 100)"
// @Param        offset    query  int     false  "Offset for pagination (default 0)"
// @Success      200  {object}  object{status=string,count=int,data=array,pagination=object{total=int,limit=int,offset=int}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      500  {object}  object{error=string}
// @Router       /admin/support/tickets/{ticketId}/messages [get]
// @Security     BearerAuth
func AdminGetTicketMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" && callerRole != "support" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ticketID, err := uuid.Parse(r.PathValue("ticketId"))
	if err != nil {
		utils.WriteError(w, "invalid ticket id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var exists bool
	db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM support_tickets WHERE id = $1)`, ticketID).Scan(&exists)
	if !exists {
		utils.WriteError(w, "ticket not found", http.StatusNotFound)
		return
	}

	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 0 {
			offset = n
		}
	}

	var total int
	db.QueryRow(ctx, `SELECT COUNT(*) FROM support_messages WHERE ticket_id = $1`, ticketID).Scan(&total)

	rows, err := db.Query(ctx, `
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

	// Mark user messages as read
	db.Exec(ctx, `
		UPDATE support_messages
		SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_type = 'user' AND is_read = FALSE
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
// Helpers
// ============================================================================

func buildAdminWSPayload(msgType string, payload interface{}) []byte {
	type outgoing struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}
	b, _ := json.Marshal(outgoing{Type: msgType, Payload: payload})
	return b
}
