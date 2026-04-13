package support

import (
	"time"

	"github.com/google/uuid"
)

type TicketStatus string
type TicketCategory string
type SenderType string

const (
	TicketStatusOpen        TicketStatus = "open"
	TicketStatusAssigned    TicketStatus = "assigned"
	TicketStatusInProgress  TicketStatus = "in_progress"
	TicketStatusWaitingUser TicketStatus = "waiting_user"
	TicketStatusResolved    TicketStatus = "resolved"
	TicketStatusClosed      TicketStatus = "closed"
)

const (
	CategoryPaymentAndRefund       TicketCategory = "payment_and_refund"
	CategoryBookingAndReservation  TicketCategory = "booking_and_reservation"
	CategoryServiceIssues          TicketCategory = "service_issues"
	CategoryDisputes               TicketCategory = "disputes"
	CategoryAccountAndVerification TicketCategory = "account_and_verification"
	CategoryTechnicalIssue         TicketCategory = "technical_issue"
	CategoryReportUserOrProperty   TicketCategory = "report_user_or_property"
	CategoryGeneralInquiry         TicketCategory = "general_inquiry"
	CategoryOther                  TicketCategory = "other"
)

const (
	SenderTypeUser  SenderType = "user"
	SenderTypeAdmin SenderType = "admin"
)

type SupportTicket struct {
	ID               uuid.UUID      `json:"id"`
	UserID           uuid.UUID      `json:"user_id"`
	JobDisputeID     *uuid.UUID     `json:"job_dispute_id,omitempty"`
	BookingDisputeID *uuid.UUID     `json:"booking_dispute_id,omitempty"`
	OrderDisputeID   *uuid.UUID     `json:"order_dispute_id,omitempty"`
	AssignedAdminID  *uuid.UUID     `json:"assigned_admin_id,omitempty"`
	Subject          string         `json:"subject"`
	Category         TicketCategory `json:"category"`
	Status           TicketStatus   `json:"status"`
	Priority         string         `json:"priority"`
	ResolvedAt       *time.Time     `json:"resolved_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type SupportMessage struct {
	ID         uuid.UUID `json:"id"`
	TicketID   uuid.UUID `json:"ticket_id"`
	SenderID   uuid.UUID `json:"sender_id"`
	SenderType string    `json:"sender_type"` // "user" | "admin"
	Content    string    `json:"content"`
	MsgType    string    `json:"msg_type"` // "text" | "image"
	IsRead     bool      `json:"is_read"`
	CreatedAt  time.Time `json:"created_at"`
}

type SupportWSIncoming struct {
	Type     string `json:"type"` // "message" | "read" | "typing"
	TicketID string `json:"ticket_id"`
	Content  string `json:"content"`
}

type SupportWSOutgoing struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
