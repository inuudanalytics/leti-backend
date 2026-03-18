package utils

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)

	if email == "" {
		return errors.New("email is required")
	}

	_, err := mail.ParseAddress(email)
	if err != nil {
		return errors.New("invalid email format")
	}

	if !emailRegex.MatchString(email) {
		return errors.New("invalid email format, must include a valid domain (e.g. .com, .ng)")
	}

	return nil
}
