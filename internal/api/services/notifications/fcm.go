package notifications

import (
	"context"

	"leti_server/pkg/apperrors"
	"leti_server/pkg/config"

	"firebase.google.com/go/v4/messaging"
)

func SendPushNotification(token, title, body string, data ...map[string]string) error {
	ctx := context.Background()

	client, err := config.FirebaseApp.Messaging(ctx)
	if err != nil {
		return err
	}

	var dataPayload map[string]string
	if len(data) > 0 && data[0] != nil {
		dataPayload = data[0]
	}

	isCallIncoming := dataPayload["type"] == "call.incoming"

	message := &messaging.Message{
		Token: token,
		Data:  dataPayload,
	}

	if !isCallIncoming {
		message.Notification = &messaging.Notification{
			Title: title,
			Body:  body,
		}
	} else {
		if message.Data == nil {
			message.Data = make(map[string]string)
		}
		message.Data["title"] = title
		message.Data["body"] = body

		message.Android = &messaging.AndroidConfig{
			Priority: "high",
		}
	}

	_, err = client.Send(ctx, message)
	if err != nil {
		if messaging.IsUnregistered(err) ||
			messaging.IsInvalidArgument(err) ||
			messaging.IsSenderIDMismatch(err) {
			return apperrors.NewInvalidOrExpiredTokenError()
		}
		return err
	}

	return err
}
