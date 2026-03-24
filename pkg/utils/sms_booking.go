package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func sendTermiiSMS(phoneNumber, message string) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration in environment variables")
	}

	payload := TermiiSMSPayload{
		To:      phoneNumber,
		From:    senderID,
		Sms:     message,
		Type:    "plain",
		Channel: "generic",
		ApiKey:  apiKey,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal SMS payload: %w", err)
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/sms/send", baseURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to send SMS via Termii: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("termii API returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}

// ============================================================================
// 1. Booking Request  →  artisan
// ============================================================================
func SendBookingRequestSMS(phoneNumber, artisanName, clientName, serviceName, bookingDate, startTime string) error {
	message := fmt.Sprintf(
		"Hi %s, you have a new booking request on Leti! %s wants to book you for %s on %s at %s. Open the app to accept or decline.",
		artisanName, clientName, serviceName, bookingDate, startTime,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 2. Booking Confirmed  →  client  (artisan accepted; payment required)
// ============================================================================
func SendBookingConfirmedSMS(phoneNumber, clientName, artisanName, serviceName, bookingDate string) error {
	message := fmt.Sprintf(
		"Hi %s, %s has confirmed your booking for %s on %s. Open the Leti app to complete payment and lock in your slot.",
		clientName, artisanName, serviceName, bookingDate,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 3. Booking Declined  →  client
// ============================================================================
func SendBookingDeclinedSMS(phoneNumber, clientName, artisanName, serviceName string) error {
	message := fmt.Sprintf(
		"Hi %s, %s is unavailable and has declined your booking request for %s. Open Leti to find another artisan.",
		clientName, artisanName, serviceName,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 4. Booking Cancelled  →  the other party
// cancelledByRole: "client" or "artisan"
// ============================================================================
func SendBookingCancelledSMS(phoneNumber, recipientName, otherPartyName, serviceName, bookingDate, cancelledByRole string) error {
	cancelledByLabel := "The client"
	if cancelledByRole == "artisan" {
		cancelledByLabel = "The artisan"
	}

	message := fmt.Sprintf(
		"Hi %s, %s (%s) has cancelled the booking for %s on %s. If a payment was made, a full refund will be processed. Open Leti for details.",
		recipientName, cancelledByLabel, otherPartyName, serviceName, bookingDate,
	)
	return sendTermiiSMS(phoneNumber, message)
}

// ============================================================================
// 5. Booking Completed  →  client
// ============================================================================
func SendBookingCompletedSMS(phoneNumber, clientName, artisanName, serviceName string) error {
	message := fmt.Sprintf(
		"Hi %s, %s has marked your %s booking as completed. Open Leti to leave a review and confirm the service.",
		clientName, artisanName, serviceName,
	)
	return sendTermiiSMS(phoneNumber, message)
}
