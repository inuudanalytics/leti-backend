package utils

import (
	"context"
	"encoding/json"
	"leti_server/internal/repositories/sqlconnect"

	"github.com/google/uuid"
)

type NotificationType string

const (
	// Bookings (shortlets)
	NotifBookingRequest    NotificationType = "booking_request"
	NotifBookingConfirmed  NotificationType = "booking_confirmed"
	NotifBookingDeclined   NotificationType = "booking_declined"
	NotifBookingCancelled  NotificationType = "booking_cancelled"
	NotifBookingCompleted  NotificationType = "booking_completed"
	NotifBookingReminder   NotificationType = "booking_reminder"
	NotifBookingCheckedIn  NotificationType = "booking_checked_in"
	NotifBookingCheckedOut NotificationType = "booking_checked_out"

	// Artisan jobs
	NotifJobRequest       NotificationType = "job_request"
	NotifJobAccepted      NotificationType = "job_accepted"
	NotifJobDeclined      NotificationType = "job_declined"
	NotifJobCancelled     NotificationType = "job_cancelled"
	NotifJobCompleted     NotificationType = "job_completed"
	NotifJobQuoteReceived NotificationType = "job_quote_received"
	NotifJobQuoteAccepted NotificationType = "job_quote_accepted"
	NotifJobQuoteRejected NotificationType = "job_quote_rejected"

	// Payments
	NotifPaymentReceived NotificationType = "payment_received"
	NotifPaymentReleased NotificationType = "payment_released"
	NotifPaymentHeld     NotificationType = "payment_held"
	NotifPaymentRefunded NotificationType = "payment_refunded"
	NotifEscrowFunded    NotificationType = "escrow_funded"

	// Reviews
	NotifReviewReceived NotificationType = "review_received"

	// Role switching
	NotifRoleActivated NotificationType = "role_activated"

	// Disputes & support
	NotifDisputeFiled          NotificationType = "dispute_filed"
	NotifDisputeResolved       NotificationType = "dispute_resolved"
	NotifSupportTicketOpened   NotificationType = "support_ticket_opened"
	NotifSupportTicketReply    NotificationType = "support_ticket_reply"
	NotifSupportTicketResolved NotificationType = "support_ticket_resolved"

	// General
	NotifGeneral NotificationType = "general"
)

func CreateNotification(ctx context.Context, userID uuid.UUID, notifType NotificationType, title, body string, data map[string]interface{}) error {
	db := sqlconnect.DB
	if db == nil {
		return nil
	}

	var dataJSON []byte
	var err error
	if data != nil {
		dataJSON, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}

	_, err = db.Exec(ctx, `
		INSERT INTO notifications (user_id, type, title, body, data)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, notifType, title, body, dataJSON)
	return err
}
