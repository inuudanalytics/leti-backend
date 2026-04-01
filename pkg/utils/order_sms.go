package utils

import "fmt"

// ============================================================================
// 1. Order Confirmed  →  client
// ============================================================================
func SendOrderConfirmedSMS(phoneNumber, clientName, propName, checkIn, checkOut string) error {
	message := fmt.Sprintf(
		"Hi %s, your booking at %s from %s to %s is confirmed on Leti! Payment is secured. Open the app for details.",
		clientName, propName, checkIn, checkOut,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 2. Order Cancelled  →  the non-cancelling party
// ============================================================================
func SendOrderCancelledSMS(phoneNumber, recipientName, cancellerName, propName string, refundProcessed bool) error {
	refundNote := ""
	if refundProcessed {
		refundNote = " A full refund has been sent to your wallet."
	}
	message := fmt.Sprintf(
		"Hi %s, %s has cancelled the booking at %s on Leti.%s Open the app for details.",
		recipientName, cancellerName, propName, refundNote,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 3. Check-in Reminder  →  client (sent 24h before)
// ============================================================================
func SendCheckinReminderSMS(phoneNumber, clientName, propName, checkIn, checkInTime string) error {
	message := fmt.Sprintf(
		"Hi %s, reminder: your stay at %s starts tomorrow (%s) at %s. Open Leti to view details.",
		clientName, propName, checkIn, checkInTime,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 4. New Booking  →  owner
// ============================================================================
func SendNewBookingSMS(phoneNumber, ownerName, clientName, propName, checkIn string) error {
	message := fmt.Sprintf(
		"Hi %s, %s just booked your property %s on Leti! Check-in: %s. Payment secured in escrow. Open the app.",
		ownerName, clientName, propName, checkIn,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 5. Check-out confirmed  →  owner
// ============================================================================
func SendCheckoutConfirmedSMS(phoneNumber, ownerName, propName string, netPayout float64) error {
	message := fmt.Sprintf(
		"Hi %s, your guest at %s has checked out. ₦%.2f has been released to your Leti wallet. Open the app to withdraw.",
		ownerName, propName, netPayout,
	)
	return sendTermiiSMS(phoneNumber, message)
}
