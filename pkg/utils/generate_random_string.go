package utils

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func GenerateRandomString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "rndfallback"
	}

	s := hex.EncodeToString(bytes)
	s = strings.ToLower(s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}
