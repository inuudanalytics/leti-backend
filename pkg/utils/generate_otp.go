package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateSecureOTP() (string, error) {
	nano := time.Now().UnixNano()
	t1 := charset[nano%int64(len(charset))]
	t2 := charset[(nano/int64(len(charset)))%int64(len(charset))]

	random := make([]byte, 4)
	for i := range random {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("failed to generate secure code: %w", err)
		}
		random[i] = charset[n.Int64()]
	}

	code := []byte{
		random[0],
		t1,
		random[1],
		random[2],
		t2,
		random[3],
	}

	return string(code), nil
}
