package utils

import (
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// GenerateShortletCode generates a unique, short agent code like Let-26-4F7G9K2L
func GenerateShortletCode() string {
	year := time.Now().Year() % 100

	u := uuid.New()

	bi := new(big.Int).SetBytes(u[:])

	code := bi.Text(36)[:8]

	return fmt.Sprintf("BDM-%02d-%s", year, code)
}
