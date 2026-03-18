package utils

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

func ErrorHandler(err error, message string) error {
	if err != nil {
		Logger.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Error(message)
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}
