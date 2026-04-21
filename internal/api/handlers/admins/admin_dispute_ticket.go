package admins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	supportModels "leti_server/internal/models/support"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// POST /api/v1/admin/disputes/{id}/ticket
// Admin escalates a dispute to a live-chat support ticket
// Query param: ?type=job|booking|order
// ============================================================================

// AdminCreateDisputeTicket godoc
// @Summary      Escalate a dispute to a live-chat support ticket (admin)
// @Description  Creates a support ticket linked to the specified dispute and assigns it to the calling admin, changing the dispute status to `investigating`. If a ticket already exists for the dispute, the existing ticket ID is returned without creating a duplicate. The user who filed the dispute receives a push notification and an in-app notification inviting them to the support chat.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                           true   "Dispute UUID"
// @Param        type  query     string                           false  "Dispute type: job, booking, or order (default: job)"
// @Param        body  body      AdminCreateDisputeTicketRequest  true   "Ticket options (subject and priority are optional)"
// @Success      200  {object}  object{status=string,message=string,data=support.SupportTicket}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}  "Dispute not found"
// @Router       /admin/disputes/{id}/ticket [post]
// @Security     BearerAuth
func AdminCreateDisputeTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	disputeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid dispute id", http.StatusBadRequest)
		return
	}

	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}

	type request struct {
		Subject  string `json:"subject"`
		Priority string `json:"priority"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Subject == "" {
		req.Subject = fmt.Sprintf("Dispute escalation — %s dispute", disputeType)
	}
	if req.Priority == "" {
		req.Priority = "high"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// ── Fetch the filer of the dispute ───────────────────────────────────────
	var filedBy uuid.UUID

	switch disputeType {
	case "job":
		err = db.QueryRow(ctx, `SELECT filed_by FROM job_disputes WHERE id = $1`, disputeID).Scan(&filedBy)
	case "booking":
		err = db.QueryRow(ctx, `SELECT filed_by FROM booking_disputes WHERE id = $1`, disputeID).Scan(&filedBy)
	case "order":
		err = db.QueryRow(ctx, `SELECT filed_by FROM order_disputes WHERE id = $1`, disputeID).Scan(&filedBy)
	default:
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
		return
	}

	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "dispute not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── Check if a ticket already exists for this dispute ────────────────────
	var existingTicketID *uuid.UUID

	switch disputeType {
	case "job":
		var tid uuid.UUID
		if err := db.QueryRow(ctx,
			`SELECT id FROM support_tickets WHERE job_dispute_id = $1 LIMIT 1`, disputeID,
		).Scan(&tid); err == nil {
			existingTicketID = &tid
		}
	case "booking":
		var tid uuid.UUID
		if err := db.QueryRow(ctx,
			`SELECT id FROM support_tickets WHERE booking_dispute_id = $1 LIMIT 1`, disputeID,
		).Scan(&tid); err == nil {
			existingTicketID = &tid
		}
	case "order":
		var tid uuid.UUID
		if err := db.QueryRow(ctx,
			`SELECT id FROM support_tickets WHERE order_dispute_id = $1 LIMIT 1`, disputeID,
		).Scan(&tid); err == nil {
			existingTicketID = &tid
		}
	}

	if existingTicketID != nil {
		utils.WriteJSON(w, map[string]interface{}{
			"status":    "success",
			"message":   "ticket already exists for this dispute",
			"ticket_id": existingTicketID,
		})
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var ticket supportModels.SupportTicket

	switch disputeType {
	case "job":
		err = tx.QueryRow(ctx, `
			INSERT INTO support_tickets
				(user_id, job_dispute_id, assigned_admin_id, subject, category, priority, status)
			VALUES ($1, $2, $3, $4, 'disputes', $5, 'assigned')
			RETURNING id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			          assigned_admin_id, subject, category, status, priority,
			          resolved_at, created_at, updated_at
		`, filedBy, disputeID, callerID, req.Subject, req.Priority).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
		if err == nil {
			tx.Exec(ctx, `UPDATE job_disputes SET status = 'investigating' WHERE id = $1`, disputeID)
		}

	case "booking":
		err = tx.QueryRow(ctx, `
			INSERT INTO support_tickets
				(user_id, booking_dispute_id, assigned_admin_id, subject, category, priority, status)
			VALUES ($1, $2, $3, $4, 'disputes', $5, 'assigned')
			RETURNING id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			          assigned_admin_id, subject, category, status, priority,
			          resolved_at, created_at, updated_at
		`, filedBy, disputeID, callerID, req.Subject, req.Priority).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
		if err == nil {
			tx.Exec(ctx, `UPDATE booking_disputes SET status = 'investigating' WHERE id = $1`, disputeID)
		}

	case "order":
		err = tx.QueryRow(ctx, `
			INSERT INTO support_tickets
				(user_id, order_dispute_id, assigned_admin_id, subject, category, priority, status)
			VALUES ($1, $2, $3, $4, 'disputes', $5, 'assigned')
			RETURNING id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			          assigned_admin_id, subject, category, status, priority,
			          resolved_at, created_at, updated_at
		`, filedBy, disputeID, callerID, req.Subject, req.Priority).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
		if err == nil {
			tx.Exec(ctx, `UPDATE order_disputes SET status = 'investigating' WHERE id = $1`, disputeID)
		}
	}

	if err != nil {
		utils.Logger.Errorf("create dispute ticket: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Notify the user
	go utils.CreateNotification(context.Background(), filedBy, utils.NotifSupportTicketOpened,
		"Dispute Under Review",
		"An admin has opened a support chat for your dispute. You can now chat with support.",
		map[string]interface{}{"ticket_id": ticket.ID, "dispute_id": disputeID},
	)
	go handlers.SendPushToUser(filedBy, "Dispute Ticket Opened",
		"A support ticket has been created for your dispute. We will review it shortly.",
		map[string]string{"screen": "SupportTicket", "ticket_id": ticket.ID.String()})

	logAudit(ctx, db, callerID, "dispute.ticket_created", disputeType+"_dispute", &disputeID,
		map[string]interface{}{"ticket_id": ticket.ID}, r)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "dispute ticket created, live chat is now open",
		"data":    ticket,
	})
}

// ============================================================================
// GET /api/v1/admin/disputes/{id}/ticket
// Admin gets the support ticket linked to a dispute (and its chat history)
// Query param: ?type=job|booking|order
// ============================================================================

// AdminGetDisputeTicket godoc
// @Summary      Get the support ticket linked to a dispute (admin)
// @Description  Returns the support ticket that was created for the given dispute together with the full chronological message history. Returns 404 if no ticket has been created yet.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        id    path      string  true   "Dispute UUID"
// @Param        type  query     string  false  "Dispute type: job, booking, or order (default: job)"
// @Success      200  {object}  object{status=string,data=support.SupportTicket,messages=array}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}  "No ticket found for this dispute"
// @Router       /admin/disputes/{id}/ticket [get]
// @Security     BearerAuth
func AdminGetDisputeTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	disputeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid dispute id", http.StatusBadRequest)
		return
	}

	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ticket supportModels.SupportTicket
	var queryErr error

	switch disputeType {
	case "job":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE job_dispute_id = $1
		`, disputeID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	case "booking":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE booking_dispute_id = $1
		`, disputeID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	case "order":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE order_dispute_id = $1
		`, disputeID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	default:
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
		return
	}

	if queryErr != nil {
		if queryErr == pgx.ErrNoRows {
			utils.WriteError(w, "no ticket found for this dispute", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Fetch messages
	rows, err := db.Query(ctx, `
		SELECT id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
		FROM support_messages
		WHERE ticket_id = $1
		ORDER BY created_at ASC
	`, ticket.ID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]supportModels.SupportMessage, 0)
	for rows.Next() {
		var m supportModels.SupportMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderID, &m.SenderType,
			&m.Content, &m.MsgType, &m.IsRead, &m.CreatedAt); err != nil {
			continue
		}
		messages = append(messages, m)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"data":     ticket,
		"messages": messages,
	})
}

// GetMyDisputeTicket godoc
// @Summary      Get my dispute support ticket
// @Description  Retrieves the support ticket and full message history for a dispute the authenticated user filed. Also marks any unread admin messages as read. Pass dispute_id and type as query params.
// @Tags         Disputes
// @Produce      json
// @Param        dispute_id  query  string  true   "Dispute UUID"
// @Param        type        query  string  false  "Dispute type: job, booking, or order (default: job)"
// @Success      200  {object}  object{status=string,data=object,messages=array}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /dispute-centre/disputes/ticket [get]
// @Security     BearerAuth
func GetMyDisputeTicket(w http.ResponseWriter, r *http.Request) {
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

	disputeIDStr := r.URL.Query().Get("dispute_id")
	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}
	if disputeIDStr == "" {
		utils.WriteError(w, "dispute_id is required", http.StatusBadRequest)
		return
	}

	disputeID, err := uuid.Parse(disputeIDStr)
	if err != nil {
		utils.WriteError(w, "invalid dispute_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ticket supportModels.SupportTicket
	var queryErr error

	switch disputeType {
	case "job":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE job_dispute_id = $1 AND user_id = $2
		`, disputeID, userID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	case "booking":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE booking_dispute_id = $1 AND user_id = $2
		`, disputeID, userID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	case "order":
		queryErr = db.QueryRow(ctx, `
			SELECT id, user_id, job_dispute_id, booking_dispute_id, order_dispute_id,
			       assigned_admin_id, subject, category, status, priority,
			       resolved_at, created_at, updated_at
			FROM support_tickets WHERE order_dispute_id = $1 AND user_id = $2
		`, disputeID, userID).Scan(
			&ticket.ID, &ticket.UserID, &ticket.JobDisputeID, &ticket.BookingDisputeID, &ticket.OrderDisputeID,
			&ticket.AssignedAdminID, &ticket.Subject, &ticket.Category,
			&ticket.Status, &ticket.Priority, &ticket.ResolvedAt,
			&ticket.CreatedAt, &ticket.UpdatedAt,
		)
	default:
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
		return
	}

	if queryErr != nil {
		if queryErr == pgx.ErrNoRows {
			utils.WriteError(w, "no support chat open for this dispute yet", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Fetch message history
	rows, err := db.Query(ctx, `
		SELECT id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
		FROM support_messages
		WHERE ticket_id = $1
		ORDER BY created_at ASC
	`, ticket.ID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]supportModels.SupportMessage, 0)
	for rows.Next() {
		var m supportModels.SupportMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderID, &m.SenderType,
			&m.Content, &m.MsgType, &m.IsRead, &m.CreatedAt); err != nil {
			continue
		}
		messages = append(messages, m)
	}

	// Mark admin messages as read
	db.Exec(ctx, `
		UPDATE support_messages SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_type = 'admin' AND is_read = FALSE
	`, ticket.ID)

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"data":     ticket,
		"messages": messages,
	})
}
