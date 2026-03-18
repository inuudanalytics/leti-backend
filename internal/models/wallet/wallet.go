package wallet

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Wallet struct {
	ID                uuid.UUID       `json:"id,omitempty" db:"id"`
	UserID            uuid.UUID       `json:"user_id,omitempty" db:"user_id"`
	Balance           decimal.Decimal `json:"balance,omitempty" db:"balance"`
	Currency          string          `json:"currency,omitempty" db:"currency"`
	WalletCreatedAt   time.Time       `json:"wallet_created_at,omitempty" db:"wallet_created_at"`
	LastTransactionAt *time.Time      `json:"last_transaction_at,omitempty" db:"last_transaction_at"`
	IsActive          bool            `json:"is_active,omitempty" db:"is_active"`
}
