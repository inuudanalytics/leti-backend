package utils

import (
	"errors"
	"regexp"
	"strings"
)

func ValidateNigerianPhone(phone string) error {
	phone = strings.TrimSpace(phone)

	regex := regexp.MustCompile(`^\+234[0-9]{10}$`)

	if !regex.MatchString(phone) {
		return errors.New("invalid phone number format: must be in +234XXXXXXXXXX format")
	}

	return nil
}
