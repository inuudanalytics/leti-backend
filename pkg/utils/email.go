package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/resend/resend-go/v3"
)

func SendEmail(to, subject, body string, attachments ...string) error {
	apiKey := os.Getenv("RESEND_API_KEY")

	client := resend.NewClient(apiKey)

	params := &resend.SendEmailRequest{
		From:    os.Getenv("RESEND_FROM_EMAIL"),
		To:      []string{to},
		Subject: subject,
		Html:    body,
	}

	if len(attachments) > 0 {
		var resendAttachments []*resend.Attachment
		for _, filePath := range attachments {
			fileData, err := os.ReadFile(filePath)
			if err != nil {
				Logger.Warnf("Attachment not found, skipping: %s", filePath)
				continue
			}
			resendAttachments = append(resendAttachments, &resend.Attachment{
				Content:  fileData,
				Filename: filepath.Base(filePath),
			})
		}
		params.Attachments = resendAttachments
	}

	sent, err := client.Emails.Send(params)
	if err != nil {
		Logger.Errorf("failed to send email to %s: %v", to, err)
		return fmt.Errorf("failed to send email: %v", err)
	}

	Logger.Infof("Email sent successfully, ID: %s", sent.Id)
	return nil
}
