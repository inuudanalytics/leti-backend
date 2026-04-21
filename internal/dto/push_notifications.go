package dto

import (
	"leti_server/internal/api/handlers"

	"github.com/google/uuid"
)

// PushBookingRequest — artisan receives a new booking request.
func PushBookingRequest(artisanID, bookingID uuid.UUID, clientUsername, serviceName string) {
	handlers.SendPushToUser(artisanID,
		"New Booking Request",
		clientUsername+" has requested a booking for "+serviceName+".",
		map[string]string{
			"screen":     "BookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingEscrowFunded — client paid; artisan can now start the job.
func PushBookingEscrowFunded(artisanID, bookingID uuid.UUID, amountStr string) {
	handlers.SendPushToUser(artisanID,
		"Payment Secured",
		"₦"+amountStr+" secured in escrow for your booking. You may begin the job.",
		map[string]string{
			"screen":     "BookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingConfirmToArtisan — confirmation that an artisan actioned a booking
// (accepted/declined/cancelled/completed from the artisan's perspective).
func PushBookingConfirmToArtisan(artisanID, bookingID uuid.UUID, title, body string) {
	handlers.SendPushToUser(artisanID, title, body,
		map[string]string{
			"screen":     "BookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingConfirmedToClient — artisan confirmed; client must pay.
func PushBookingConfirmedToClient(clientID, bookingID uuid.UUID, artisanUsername string) {
	handlers.SendPushToUser(clientID,
		"Booking Confirmed – Payment Required",
		artisanUsername+" has accepted your booking. Tap to complete payment.",
		map[string]string{
			"screen":     "ClientBookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingDeclinedToClient — artisan declined the booking request.
func PushBookingDeclinedToClient(clientID, bookingID uuid.UUID, artisanUsername string) {
	handlers.SendPushToUser(clientID,
		"Booking Declined",
		artisanUsername+" has declined your booking request.",
		map[string]string{
			"screen":     "ClientBookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingCancelledToClient — booking was cancelled (by either party).
func PushBookingCancelledToClient(clientID, bookingID uuid.UUID, cancellerRole string) {
	body := "Your booking has been cancelled."
	if cancellerRole == "artisan" {
		body = "The artisan has cancelled your booking."
	}
	handlers.SendPushToUser(clientID,
		"Booking Cancelled",
		body,
		map[string]string{
			"screen":     "ClientBookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushBookingCompletedToClient — client confirmation requested or booking done.
func PushBookingCompletedToClient(clientID, bookingID uuid.UUID, artisanUsername string) {
	handlers.SendPushToUser(clientID,
		"Confirm Booking Completion",
		artisanUsername+" has marked your booking complete. Tap to confirm.",
		map[string]string{
			"screen":     "ClientBookingDetail",
			"booking_id": bookingID.String(),
		})
}

// PushArtisanPaymentReleased — escrow released to artisan after completion.
func PushArtisanPaymentReleased(artisanID uuid.UUID, netPayoutStr string) {
	handlers.SendPushToUser(artisanID,
		"Payment Released",
		"₦"+netPayoutStr+" has been released to your wallet (after platform fee).",
		map[string]string{
			"screen": "ArtisanWallet",
		})
}

// PushArtisanPaymentRefunded — escrow refunded to artisan (e.g. on dispute resolution).
func PushArtisanPaymentRefunded(artisanID uuid.UUID, amountStr string) {
	handlers.SendPushToUser(artisanID,
		"Payment Refunded",
		"₦"+amountStr+" has been refunded to your wallet.",
		map[string]string{
			"screen": "ArtisanWallet",
		})
}

// PushOwnerPaymentReleased — escrow released to owner after checkout.
func PushOwnerPaymentReleased(ownerID uuid.UUID, netPayoutStr string) {
	handlers.SendPushToUser(ownerID,
		"Payment Released",
		"₦"+netPayoutStr+" has been released to your wallet (after platform fee).",
		map[string]string{
			"screen": "OwnerWallet",
		})
}

// PushOwnerPaymentRefunded — escrow refunded to owner.
func PushOwnerPaymentRefunded(ownerID uuid.UUID, amountStr string) {
	handlers.SendPushToUser(ownerID,
		"Payment Refunded",
		"₦"+amountStr+" has been refunded to your wallet.",
		map[string]string{
			"screen": "OwnerWallet",
		})
}

// PushOrderConfirmedToClient — shortlet order confirmed and paid.
func PushOrderConfirmedToClient(clientID, orderID uuid.UUID, propertyName string) {
	handlers.SendPushToUser(clientID,
		"Booking Confirmed",
		"Your booking at "+propertyName+" is confirmed. See you there!",
		map[string]string{
			"screen":   "ClientOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushOrderCancelledToClient — order was cancelled.
func PushOrderCancelledToClient(clientID, orderID uuid.UUID) {
	handlers.SendPushToUser(clientID,
		"Booking Cancelled",
		"Your shortlet booking has been cancelled.",
		map[string]string{
			"screen":   "ClientOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushCheckedInToClient — owner marked guest as checked in.
func PushCheckedInToClient(clientID, orderID uuid.UUID, propertyName string) {
	handlers.SendPushToUser(clientID,
		"You're Checked In!",
		"Your check-in at "+propertyName+" has been confirmed. Enjoy your stay!",
		map[string]string{
			"screen":   "ClientOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushCheckedOutToClient — owner marked guest as checked out.
func PushCheckedOutToClient(clientID, orderID uuid.UUID) {
	handlers.SendPushToUser(clientID,
		"Your Stay is Complete",
		"Your check-out has been confirmed. Please leave a review!",
		map[string]string{
			"screen":   "ClientOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushNewOrderToOwner — a client just booked the owner's property.
func PushNewOrderToOwner(ownerID, orderID uuid.UUID, clientUsername string) {
	handlers.SendPushToUser(ownerID,
		"New Booking Request",
		clientUsername+" has booked your property.",
		map[string]string{
			"screen":   "OwnerOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushOwnerGuestCheckedIn — confirmation push to the owner that guest checked in.
func PushOwnerGuestCheckedIn(ownerID, orderID uuid.UUID) {
	handlers.SendPushToUser(ownerID,
		"Guest Checked In",
		"Your guest has been checked in successfully.",
		map[string]string{
			"screen":   "OwnerOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushOwnerGuestCheckedOut — confirmation push to the owner that guest checked out.
func PushOwnerGuestCheckedOut(ownerID, orderID uuid.UUID) {
	handlers.SendPushToUser(ownerID,
		"Guest Checked Out",
		"Your guest has checked out. Payment will be released to your wallet.",
		map[string]string{
			"screen":   "OwnerOrderDetail",
			"order_id": orderID.String(),
		})
}

// PushAdCampaignEvent — campaign started, paused, ended, or low balance.
func PushAdCampaignEvent(userID, campaignID uuid.UUID, title, body string) {
	handlers.SendPushToUser(userID, title, body,
		map[string]string{
			"screen":      "AdCenter",
			"campaign_id": campaignID.String(),
		})
}

// PushAdInsufficientBalance — campaign auto-paused due to low wallet balance.
func PushAdInsufficientBalance(userID, campaignID uuid.UUID, requiredStr string) {
	handlers.SendPushToUser(userID,
		"Ad Campaign Paused",
		"Top up at least ₦"+requiredStr+" to resume your campaign.",
		map[string]string{
			"screen":      "AdCenter",
			"campaign_id": campaignID.String(),
		})
}

// PushSupportTicketReply — admin replied to a user's support ticket.
func PushSupportTicketReply(userID, ticketID uuid.UUID) {
	handlers.SendPushToUser(userID,
		"Support Reply Received",
		"An admin has replied to your support ticket.",
		map[string]string{
			"screen":    "SupportTicket",
			"ticket_id": ticketID.String(),
		})
}

// PushDisputeTicketCreated — a dispute ticket has been opened for the user.
func PushDisputeTicketCreated(userID, ticketID uuid.UUID) {
	handlers.SendPushToUser(userID,
		"Dispute Ticket Opened",
		"A support ticket has been created for your dispute. We will review it shortly.",
		map[string]string{
			"screen":    "SupportTicket",
			"ticket_id": ticketID.String(),
		})
}

// PushDisputeTicketResolved — admin resolved a dispute ticket.
func PushDisputeTicketResolved(userID, ticketID uuid.UUID, resolution string) {
	handlers.SendPushToUser(userID,
		"Dispute Resolved",
		"Your dispute has been resolved: "+resolution,
		map[string]string{
			"screen":    "SupportTicket",
			"ticket_id": ticketID.String(),
		})
}

func PushFallback(userID uuid.UUID, title, body string) {
	handlers.SendPushToUser(userID, title, body, nil)
}

func PushWalletCredited(userID uuid.UUID, amountStr string) {
	PushFallback(userID,
		"Wallet Funded",
		"Your wallet has been credited with ₦"+amountStr+".",
	)
}

// PushWithdrawalInProgress — transfer is queued by Paystack (fallback).
func PushWithdrawalInProgress(userID uuid.UUID) {
	PushFallback(userID,
		"Withdrawal In Progress",
		"Your withdrawal is being processed. Funds should arrive in your bank account shortly.",
	)
}

// PushWithdrawalSucceeded — transfer confirmed by Paystack (fallback).
func PushWithdrawalSucceeded(userID uuid.UUID) {
	PushFallback(userID,
		"Withdrawal Successful",
		"Your withdrawal has been sent to your bank account.",
	)
}

// PushWithdrawalFailed — transfer failed or reversed (fallback).
func PushWithdrawalFailed(userID uuid.UUID, reason string) {
	PushFallback(userID,
		"Withdrawal Failed",
		reason,
	)
}
