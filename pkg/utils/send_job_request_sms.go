package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func SendJobRequestSMS(phoneNumber, mechanicName, ownerName, issueLabel string) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration in environment variables")
	}

	message := fmt.Sprintf(
		"Hi %s, you have a new job request on BrodaMeko! %s needs help with: %s. Open the app to accept or decline.",
		mechanicName,
		ownerName,
		issueLabel,
	)

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
		return fmt.Errorf("termii api returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}
