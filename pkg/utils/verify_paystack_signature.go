package utils

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"os"
)

func VerifyPaystackSignature(signature string, body []byte) bool {
	secret := os.Getenv("PAYSTACK_SECRET_KEY")
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}
