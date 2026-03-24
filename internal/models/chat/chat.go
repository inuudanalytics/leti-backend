package chat

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Conversation struct {
	ID        uuid.UUID  `json:"id"`
	OwnerID   uuid.UUID  `json:"owner_id"`
	ArtisanID uuid.UUID  `json:"artisan_id"`
	JobID     uuid.UUID  `json:"job_id"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Message struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	SenderID       uuid.UUID `json:"sender_id"`
	SenderRole     string    `json:"sender_role"`
	Content        string    `json:"content"`
	MsgType        string    `json:"msg_type"`
	IsRead         bool      `json:"is_read"`
	CreatedAt      time.Time `json:"created_at"`
}

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
