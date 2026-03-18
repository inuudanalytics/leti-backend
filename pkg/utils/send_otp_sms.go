package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type TermiiSMSPayload struct {
	To      string `json:"to"`
	From    string `json:"from"`
	Sms     string `json:"sms"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	ApiKey  string `json:"api_key"`
}

func SendOTPSMS(phoneNumber, fullName, otp string, expiry time.Time) error {
	apiKey := os.Getenv("TERMII_API_KEY")
	baseURL := os.Getenv("TERMII_BASE_URL")
	senderID := os.Getenv("TERMII_SENDER_ID")

	if apiKey == "" || baseURL == "" || senderID == "" {
		return fmt.Errorf("missing Termii configuration in environment variables")
	}

	loc, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		return fmt.Errorf("failed to load Lagos timezone: %w", err)
	}

	message := fmt.Sprintf(
		"Hi %s, your Leti verification code is %s. It expires at %s. Do not share this code with anyone.",
		fullName,
		otp,
		expiry.In(loc).Format("3:04 PM"),
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
