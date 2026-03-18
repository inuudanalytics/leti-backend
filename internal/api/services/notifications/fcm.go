package notifications

import (
	"context"

	"leti_server/pkg/apperrors"
	"leti_server/pkg/config"

	"firebase.google.com/go/v4/messaging"
)

func SendPushNotification(token, title, body string) error {
	ctx := context.Background()

	client, err := config.FirebaseApp.Messaging(ctx)
	if err != nil {
		return err
	}

	message := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}

	_, err = client.Send(ctx, message)
	if err != nil {
		if messaging.IsUnregistered(err) || messaging.IsInvalidArgument(err) {
			return apperrors.NewInvalidOrExpiredTokenError()
		}
		return err
	}

	return err
}
