package shortletchat

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ShortletConversation links a client and an owner through a specific order.
// One conversation per order is enforced via UNIQUE (order_id).
type ShortletConversation struct {
	ID            uuid.UUID  `json:"id"`
	ClientID      uuid.UUID  `json:"client_id"`
	OwnerID       uuid.UUID  `json:"owner_id"`
	OrderID       uuid.UUID  `json:"order_id"`
	ChatExpiresAt *time.Time `json:"chat_expires_at,omitempty"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ShortletMessage is a single chat message within a ShortletConversation.
type ShortletMessage struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	SenderID       uuid.UUID `json:"sender_id"`
	SenderRole     string    `json:"sender_role"` // "client" | "owner"
	Content        string    `json:"content"`
	MsgType        string    `json:"msg_type"` // "text" | "image"
	IsRead         bool      `json:"is_read"`
	CreatedAt      time.Time `json:"created_at"`
}

// ── WebSocket wire types ────────────────────────────────────────────────────

type IncomingWS struct {
	Type           string          `json:"type"` // "message" | "read" | "typing"
	ConversationID string          `json:"conversation_id"`
	Content        string          `json:"content"`
	RawPayload     json.RawMessage `json:"payload,omitempty"`
}

type OutgoingWS struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type TypingPayload struct {
	ConversationID string    `json:"conversation_id"`
	SenderID       uuid.UUID `json:"sender_id"`
}

type ReadPayload struct {
	ConversationID string    `json:"conversation_id"`
	ReaderID       uuid.UUID `json:"reader_id"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
