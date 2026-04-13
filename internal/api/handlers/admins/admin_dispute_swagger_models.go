package admins

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// REQUEST BODY MODELS
// Exported named structs so swaggo can generate full JSON schemas.
// None of these contain runtime logic — they exist purely for documentation.
// ============================================================================

// AdminUpdateDisputeStatusRequest is the request body for
// PATCH /admin/disputes/{id}/status
type AdminUpdateDisputeStatusRequest struct {
	// Status must be "open" or "investigating"
	// Example: investigating
	Status string `json:"status" enums:"open,investigating" example:"investigating"`

	// AdminNotes is an optional internal note attached to the dispute
	// Example: Reviewing uploaded evidence from both parties
	AdminNotes string `json:"admin_notes,omitempty" example:"Reviewing uploaded evidence from both parties"`
}

// AdminCreateDisputeTicketRequest is the request body for
// POST /admin/disputes/{id}/ticket
type AdminCreateDisputeTicketRequest struct {
	// Subject is the ticket title shown in the support chat.
	// Defaults to "Dispute escalation — <type> dispute" when omitted.
	// Example: Regarding your incomplete job dispute
	Subject string `json:"subject,omitempty" example:"Regarding your incomplete job dispute"`

	// Priority is the ticket urgency level. Defaults to "high" when omitted.
	// Example: high
	Priority string `json:"priority,omitempty" enums:"low,medium,high,urgent" example:"high"`
}

// AdminDisputeDecisionRequest is the request body for
// POST /admin/disputes/{id}/decision
type AdminDisputeDecisionRequest struct {
	// Action determines how the escrowed funds are distributed.
	// - refund_full    → entire escrow refunded to payer
	// - release_full   → entire escrow (minus platform commission) released to payee
	// - refund_partial → Amount refunded to payer; remainder (minus commission) released to payee
	// - dismiss        → dispute closed with no fund movement
	// Example: refund_partial
	Action string `json:"action" enums:"refund_full,release_full,refund_partial,dismiss" example:"refund_partial"`

	// Amount is the naira value to refund to the payer.
	// Required only when Action is "refund_partial". Must be > 0 and ≤ escrow amount.
	// Example: 15000.00
	Amount float64 `json:"amount,omitempty" example:"15000.00"`

	// AdminNotes is an internal note recorded on the dispute record (not shown to users).
	// Example: Artisan only completed 60% of the work based on photo evidence.
	AdminNotes string `json:"admin_notes,omitempty" example:"Artisan only completed 60% of the work based on photo evidence."`

	// Resolution is the public-facing explanation sent with notifications.
	// Falls back to AdminNotes when omitted.
	// Example: Partial refund granted as the job was only partially completed.
	Resolution string `json:"resolution,omitempty" example:"Partial refund granted as the job was only partially completed."`
}

// ============================================================================
// RESPONSE MODELS
// ============================================================================

// DisputeTicketResponse is the success envelope returned by
// POST /admin/disputes/{id}/ticket (new ticket created).
type DisputeTicketResponse struct {
	Status  string               `json:"status"  example:"success"`
	Message string               `json:"message" example:"dispute ticket created, live chat is now open"`
	Data    SupportTicketSwagger `json:"data"`
}

// ExistingDisputeTicketResponse is returned when a ticket already exists.
type ExistingDisputeTicketResponse struct {
	Status   string    `json:"status"    example:"success"`
	Message  string    `json:"message"   example:"ticket already exists for this dispute"`
	TicketID uuid.UUID `json:"ticket_id" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
}

// DisputeTicketWithMessagesResponse is the envelope for
// GET /admin/disputes/{id}/ticket and GET /disputes/ticket
type DisputeTicketWithMessagesResponse struct {
	Status   string                  `json:"status"   example:"success"`
	Data     SupportTicketSwagger    `json:"data"`
	Messages []SupportMessageSwagger `json:"messages"`
}

// AdminListDisputesResponse is the envelope for GET /admin/disputes
type AdminListDisputesResponse struct {
	Status     string                   `json:"status" example:"success"`
	Stats      AdminDisputeStatsSwagger `json:"stats"`
	Count      int                      `json:"count"  example:"12"`
	Data       []AdminDisputeRowSwagger `json:"data"`
	Pagination PaginationSwagger        `json:"pagination"`
}

// AdminDisputeStatsSwagger is the aggregate stats block inside AdminListDisputesResponse.
type AdminDisputeStatsSwagger struct {
	Open          int `json:"open"          example:"5"`
	Investigating int `json:"investigating" example:"3"`
	Resolved      int `json:"resolved"      example:"24"`
}

// AdminDisputeRowSwagger is a single row in the dispute list.
type AdminDisputeRowSwagger struct {
	ID         uuid.UUID  `json:"id"                    example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	Type       string     `json:"type"                  example:"job"                                   enums:"job,booking,order"`
	RefID      uuid.UUID  `json:"ref_id"                example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledBy    uuid.UUID  `json:"filed_by"              example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledName  string     `json:"filed_by_name"         example:"Amara Obi"`
	Respondent *uuid.UUID `json:"respondent_id,omitempty" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	Reason     string     `json:"reason"                example:"Artisan abandoned the job mid-way"`
	Status     string     `json:"status"                example:"open"                                  enums:"open,investigating,resolved_refund,resolved_release,dismissed"`
	CreatedAt  time.Time  `json:"created_at"            example:"2024-11-01T10:00:00Z"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty" example:"2024-11-05T14:30:00Z"`
}

// AdminGetDisputeJobResponse is the detail response for a job dispute.
type AdminGetDisputeJobResponse struct {
	Status string              `json:"status" example:"success"`
	Type   string              `json:"type"   example:"job"`
	Data   AdminJobDisputeData `json:"data"`
}

// AdminJobDisputeData is the full detail payload for a job dispute.
type AdminJobDisputeData struct {
	ID             uuid.UUID  `json:"id"                      example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	JobID          uuid.UUID  `json:"job_id"                  example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledBy        uuid.UUID  `json:"filed_by"                example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledName      string     `json:"filed_by_name"           example:"Amara Obi"`
	FiledRole      string     `json:"filed_by_role"           example:"client"`
	RespondentID   *uuid.UUID `json:"respondent_id,omitempty" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	RespondentName *string    `json:"respondent_name,omitempty" example:"Chidi Eze"`
	Reason         string     `json:"reason"                  example:"Artisan abandoned the job mid-way"`
	Evidence       *string    `json:"evidence,omitempty"      example:"[{\"url\":\"https://res.cloudinary.com/demo/image/upload/sample.jpg\",\"public_id\":\"disputes/sample\"}]"`
	Status         string     `json:"status"                  example:"investigating"                         enums:"open,investigating,resolved_refund,resolved_release,dismissed"`
	AdminNotes     *string    `json:"admin_notes,omitempty"   example:"Reviewing evidence from both parties"`
	Resolution     *string    `json:"resolution,omitempty"    example:"Partial refund granted"`
	CreatedAt      time.Time  `json:"created_at"              example:"2024-11-01T10:00:00Z"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"   example:"2024-11-05T14:30:00Z"`
	EscrowAmount   *float64   `json:"escrow_amount,omitempty" example:"50000.00"`
	EscrowStatus   *string    `json:"escrow_status,omitempty" example:"held"`
}

// AdminGetDisputeBookingResponse is the detail response for a booking dispute.
type AdminGetDisputeBookingResponse struct {
	Status string                  `json:"status" example:"success"`
	Type   string                  `json:"type"   example:"booking"`
	Data   AdminBookingDisputeData `json:"data"`
}

// AdminBookingDisputeData is the full detail payload for a booking dispute.
type AdminBookingDisputeData struct {
	ID             uuid.UUID  `json:"id"                       example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	BookingID      uuid.UUID  `json:"booking_id"               example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledBy        uuid.UUID  `json:"filed_by"                 example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledName      string     `json:"filed_by_name"            example:"Ngozi Adeyemi"`
	FiledRole      string     `json:"filed_by_role"            example:"client"`
	RespondentID   *uuid.UUID `json:"respondent_id,omitempty"  example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	RespondentName *string    `json:"respondent_name,omitempty" example:"Emeka Nwosu"`
	Reason         string     `json:"reason"                   example:"Service was not delivered as agreed"`
	Evidence       *string    `json:"evidence,omitempty"`
	Status         string     `json:"status"                   example:"open" enums:"open,investigating,resolved_refund,resolved_release,dismissed"`
	AdminNotes     *string    `json:"admin_notes,omitempty"`
	Resolution     *string    `json:"resolution,omitempty"`
	CreatedAt      time.Time  `json:"created_at"               example:"2024-11-01T10:00:00Z"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	EscrowAmount   *float64   `json:"escrow_amount,omitempty"  example:"30000.00"`
	NetPayout      *float64   `json:"escrow_net_payout,omitempty" example:"27600.00"`
	EscrowStatus   *string    `json:"escrow_status,omitempty"  example:"held"`
}

// AdminGetDisputeOrderResponse is the detail response for an order dispute.
type AdminGetDisputeOrderResponse struct {
	Status string                `json:"status" example:"success"`
	Type   string                `json:"type"   example:"order"`
	Data   AdminOrderDisputeData `json:"data"`
}

// AdminOrderDisputeData is the full detail payload for an order (shortlet) dispute.
type AdminOrderDisputeData struct {
	ID             uuid.UUID  `json:"id"                       example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	OrderID        uuid.UUID  `json:"order_id"                 example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledBy        uuid.UUID  `json:"filed_by"                 example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	FiledName      string     `json:"filed_by_name"            example:"Temi Balogun"`
	FiledRole      string     `json:"filed_by_role"            example:"client"`
	RespondentID   *uuid.UUID `json:"respondent_id,omitempty"  example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	RespondentName *string    `json:"respondent_name,omitempty" example:"Femi Adebayo"`
	Reason         string     `json:"reason"                   example:"Property condition did not match listing photos"`
	Evidence       *string    `json:"evidence,omitempty"`
	Status         string     `json:"status"                   example:"investigating" enums:"open,investigating,resolved_refund,resolved_release,dismissed"`
	AdminNotes     *string    `json:"admin_notes,omitempty"`
	Resolution     *string    `json:"resolution,omitempty"`
	CreatedAt      time.Time  `json:"created_at"               example:"2024-11-01T10:00:00Z"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	EscrowAmount   *float64   `json:"escrow_amount,omitempty"  example:"120000.00"`
	NetPayout      *float64   `json:"escrow_net_payout,omitempty" example:"110400.00"`
	EscrowStatus   *string    `json:"escrow_status,omitempty"  example:"held"`
}

// SupportTicketSwagger mirrors support.SupportTicket for Swagger schema generation.
type SupportTicketSwagger struct {
	ID               uuid.UUID  `json:"id"                         example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	UserID           uuid.UUID  `json:"user_id"                    example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	JobDisputeID     *uuid.UUID `json:"job_dispute_id,omitempty"   example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	BookingDisputeID *uuid.UUID `json:"booking_dispute_id,omitempty" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	OrderDisputeID   *uuid.UUID `json:"order_dispute_id,omitempty" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	AssignedAdminID  *uuid.UUID `json:"assigned_admin_id,omitempty" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	// Subject is the ticket title
	Subject string `json:"subject"  example:"Dispute escalation — job dispute"`
	// Category is always "disputes" for dispute-linked tickets
	Category string `json:"category" example:"disputes" enums:"payment_and_refund,booking_and_reservation,service_issues,disputes,account_and_verification,technical_issue,report_user_or_property,general_inquiry,other"`
	// Status reflects the current state of the support conversation
	Status string `json:"status"   example:"assigned" enums:"open,assigned,in_progress,waiting_user,resolved,closed"`
	// Priority is set by the admin; defaults to "high" for dispute escalations
	Priority string `json:"priority" example:"high"     enums:"low,medium,high,urgent"`

	ResolvedAt *time.Time `json:"resolved_at,omitempty" example:"2024-11-05T14:30:00Z"`
	CreatedAt  time.Time  `json:"created_at"            example:"2024-11-01T10:00:00Z"`
	UpdatedAt  time.Time  `json:"updated_at"            example:"2024-11-03T08:15:00Z"`
}

// SupportMessageSwagger mirrors support.SupportMessage for Swagger schema generation.
type SupportMessageSwagger struct {
	ID       uuid.UUID `json:"id"          example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	TicketID uuid.UUID `json:"ticket_id"   example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	SenderID uuid.UUID `json:"sender_id"   example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	// SenderType indicates whether the message was sent by a user or an admin
	SenderType string `json:"sender_type" example:"admin"  enums:"user,admin"`
	Content    string `json:"content"     example:"Can you provide more details about the incomplete work?"`
	// MsgType is "text" for plain messages and "image" for image uploads
	MsgType   string    `json:"msg_type"    example:"text"   enums:"text,image"`
	IsRead    bool      `json:"is_read"     example:"false"`
	CreatedAt time.Time `json:"created_at"  example:"2024-11-01T10:05:00Z"`
}

// PaginationSwagger is the shared pagination envelope used across list endpoints.
type PaginationSwagger struct {
	Total      int `json:"total"       example:"48"`
	Page       int `json:"page"        example:"1"`
	Limit      int `json:"limit"       example:"20"`
	TotalPages int `json:"total_pages" example:"3"`
}

// ============================================================================
// GODOC ANNOTATIONS
// These are attached to stub functions that satisfy the swaggo scanner.
// The real handler implementations live in the handler files.
// ============================================================================

// swaggerAdminListDisputes godoc
// @Summary      List all disputes (admin)
// @Description  Returns a paginated list of disputes across all three types (job, booking, order) along with aggregate status counts. Filter by type and/or status. When no type filter is supplied, results from all three tables are merged and re-paginated.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        type    query     string  false  "Dispute type filter"                                  Enums(job, booking, order)
// @Param        status  query     string  false  "Status filter"                                        Enums(open, investigating, resolved_refund, resolved_release, dismissed)
// @Param        page    query     int     false  "Page number (default: 1)"                             minimum(1)
// @Param        limit   query     int     false  "Items per page (default: 20, max: 100)"               minimum(1) maximum(100)
// @Success      200  {object}  AdminListDisputesResponse
// @Failure      403  {object}  object{error=string}  "Caller is not an admin or super_admin"
// @Router       /admin/disputes [get]
// @Security     BearerAuth

// swaggerAdminGetDispute godoc
// @Summary      Get a single dispute detail (admin)
// @Description  Returns the complete detail record for a dispute including filer info, respondent info, reason, evidence JSON, escrow snapshot, admin notes, and resolution. The shape of `data` varies by dispute type — see AdminJobDisputeData, AdminBookingDisputeData, and AdminOrderDisputeData schemas.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        id    path      string  true   "Dispute UUID"
// @Param        type  query     string  false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Success      200  {object}  AdminGetDisputeJobResponse     "Returned when type=job"
// @Success      200  {object}  AdminGetDisputeBookingResponse "Returned when type=booking"
// @Success      200  {object}  AdminGetDisputeOrderResponse   "Returned when type=order"
// @Failure      400  {object}  object{error=string}  "Invalid dispute ID or unknown type"
// @Failure      403  {object}  object{error=string}  "Forbidden"
// @Failure      404  {object}  object{error=string}  "Dispute not found"
// @Router       /admin/disputes/{id} [get]
// @Security     BearerAuth

// swaggerAdminUpdateDisputeStatus godoc
// @Summary      Update a dispute's status (admin)
// @Description  Sets a dispute's status to `open` or `investigating` and optionally saves an internal admin note. Use this before issuing a final resolution to indicate the dispute is actively being reviewed. This does NOT move any funds — use POST /admin/disputes/{id}/decision for that.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                           true   "Dispute UUID"
// @Param        type  query     string                           false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Param        body  body      AdminUpdateDisputeStatusRequest  true   "Status update payload"
// @Success      200  {object}  object{status=string,message=string}  "Example: {\"status\":\"success\",\"message\":\"dispute status updated\"}"
// @Failure      400  {object}  object{error=string}  "Invalid body, unknown status value, or unknown dispute type"
// @Failure      401  {object}  object{error=string}  "Unauthorized"
// @Failure      403  {object}  object{error=string}  "Forbidden — caller is not an admin"
// @Failure      404  {object}  object{error=string}  "Dispute not found"
// @Router       /admin/disputes/{id}/status [patch]
// @Security     BearerAuth

// swaggerAdminResolveDispute godoc
// @Summary      Issue a final decision on a dispute (admin)
// @Description  Resolves a dispute by disbursing escrowed funds according to the chosen action. The dispute and its parent record (job / booking / order) are updated to reflect the outcome, and both parties receive in-app and push notifications.\n\n**Actions:**\n- `refund_full` — entire escrow refunded to payer; platform commission waived\n- `release_full` — escrow released to payee minus 8% platform commission\n- `refund_partial` — `amount` refunded to payer; remainder released to payee minus commission. `amount` field is **required** and must be > 0 and ≤ escrow balance\n- `dismiss` — dispute closed with no fund movement (use when dispute is frivolous)\n\nOnce resolved a dispute cannot be re-resolved.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                       true   "Dispute UUID"
// @Param        type  query     string                       false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Param        body  body      AdminDisputeDecisionRequest  true   "Decision payload"
// @Success      200  {object}  object{status=string,message=string}  "Example: {\"status\":\"success\",\"message\":\"dispute resolved with action: refund_partial\"}"
// @Failure      400  {object}  object{error=string}  "Invalid action, missing amount for partial refund, amount exceeds escrow, or dispute already resolved"
// @Failure      401  {object}  object{error=string}  "Unauthorized"
// @Failure      403  {object}  object{error=string}  "Forbidden"
// @Failure      404  {object}  object{error=string}  "Dispute not found"
// @Router       /admin/disputes/{id}/decision [post]
// @Security     BearerAuth

// swaggerAdminCreateDisputeTicket godoc
// @Summary      Escalate a dispute to a live-chat support ticket (admin)
// @Description  Creates a support ticket linked to the given dispute and assigns it to the calling admin. The dispute's status is automatically set to `investigating`. If a ticket already exists, the existing ticket_id is returned in `ExistingDisputeTicketResponse` format with no duplicate created.\n\nThe user who filed the dispute receives both an in-app notification and a push notification directing them to the support chat screen.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                           true   "Dispute UUID"
// @Param        type  query     string                           false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Param        body  body      AdminCreateDisputeTicketRequest  true   "Ticket creation options — both fields are optional"
// @Success      200  {object}  DisputeTicketResponse          "New ticket created"
// @Success      200  {object}  ExistingDisputeTicketResponse  "Ticket already existed — no duplicate created"
// @Failure      400  {object}  object{error=string}  "Invalid dispute ID, malformed body, or unknown dispute type"
// @Failure      401  {object}  object{error=string}  "Unauthorized"
// @Failure      403  {object}  object{error=string}  "Forbidden — caller is not an admin"
// @Failure      404  {object}  object{error=string}  "Dispute not found"
// @Router       /admin/disputes/{id}/ticket [post]
// @Security     BearerAuth

// swaggerAdminGetDisputeTicket godoc
// @Summary      Get the support ticket linked to a dispute (admin)
// @Description  Returns the support ticket created for the given dispute along with its full chronological message history. The `messages` array is empty if no messages have been sent yet. Returns 404 if no ticket has been created — call POST /admin/disputes/{id}/ticket first.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        id    path      string  true   "Dispute UUID"
// @Param        type  query     string  false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Success      200  {object}  DisputeTicketWithMessagesResponse
// @Failure      400  {object}  object{error=string}  "Invalid dispute ID or unknown type"
// @Failure      403  {object}  object{error=string}  "Forbidden"
// @Failure      404  {object}  object{error=string}  "No ticket found for this dispute"
// @Router       /admin/disputes/{id}/ticket [get]
// @Security     BearerAuth

// swaggerGetMyDisputeTicket godoc
// @Summary      Get my dispute support ticket (user)
// @Description  Returns the support ticket and full message history for a dispute the authenticated user filed. Unread admin messages are automatically marked as read on fetch. Returns 404 if an admin has not yet opened a support chat for this dispute.
// @Tags         Disputes
// @Produce      json
// @Param        dispute_id  query  string  true   "Dispute UUID"
// @Param        type        query  string  false  "Dispute type (default: job)"  Enums(job, booking, order)
// @Success      200  {object}  DisputeTicketWithMessagesResponse
// @Failure      400  {object}  object{error=string}  "Missing or invalid dispute_id"
// @Failure      401  {object}  object{error=string}  "Unauthorized"
// @Failure      404  {object}  object{error=string}  "No support chat open for this dispute yet"
// @Router       /disputes/ticket [get]
// @Security     BearerAuth
