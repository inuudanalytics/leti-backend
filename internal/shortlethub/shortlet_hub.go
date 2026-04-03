package shortlethub

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	shortletchat "leti_server/internal/models/shortlet_chat"
	"leti_server/internal/repositories/sqlconnect"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// ============================================================================
// Client
// ============================================================================

// Client represents a single WebSocket connection attached to the hub.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	UserID uuid.UUID
	Role   string // "client" or "owner"
}

// ReadPump pumps messages from the WebSocket connection into the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[shortlet-hub] read error user %s: %v", c.UserID, err)
			}
			break
		}
		c.hub.inbound <- &inboundEnvelope{client: c, raw: raw}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ============================================================================
// Hub
// ============================================================================

type inboundEnvelope struct {
	client *Client
	raw    []byte
}

// Hub maintains the set of active clients and broadcasts messages between them.
type Hub struct {
	mu         sync.RWMutex
	clients    map[uuid.UUID]*Client
	register   chan *Client
	unregister chan *Client
	inbound    chan *inboundEnvelope
}

// New creates a ready-to-run Hub.
func New() *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]*Client),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		inbound:    make(chan *inboundEnvelope, 256),
	}
}

// NewClient builds a Client, enqueues registration, and returns it.
// The caller is responsible for starting ReadPump and WritePump goroutines.
func (h *Hub) NewClient(conn *websocket.Conn, userID uuid.UUID, role string) *Client {
	c := &Client{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 256),
		UserID: userID,
		Role:   role,
	}
	h.register <- c
	return c
}

// Run is the hub's main event loop. Call it in a dedicated goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.UserID] = c
			h.mu.Unlock()
			log.Printf("[shortlet-hub] connected: %s (%s)", c.UserID, c.Role)

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.UserID]; ok {
				delete(h.clients, c.UserID)
				close(c.send)
			}
			h.mu.Unlock()
			log.Printf("[shortlet-hub] disconnected: %s", c.UserID)

		case env := <-h.inbound:
			h.handleInbound(env)
		}
	}
}

// DeliverTo pushes a pre-encoded payload to a specific user if they are online.
func (h *Hub) DeliverTo(userID uuid.UUID, payload []byte) {
	h.sendTo(userID, payload)
}

// ============================================================================
// Push notification hook — set in main after hub initialisation
// ============================================================================

// PushNotifier can be set to deliver out-of-band push notifications to offline users.
var PushNotifier func(userID uuid.UUID, title, body string, data ...map[string]string)

// ============================================================================
// Inbound routing
// ============================================================================

func (h *Hub) handleInbound(env *inboundEnvelope) {
	var in shortletchat.IncomingWS
	if err := json.Unmarshal(env.raw, &in); err != nil {
		h.sendError(env.client, "INVALID_JSON", "message must be valid JSON")
		return
	}

	switch in.Type {
	case "message":
		h.handleMessage(env.client, &in)
	case "read":
		h.handleRead(env.client, &in)
	case "typing":
		h.handleTyping(env.client, &in)
	default:
		h.sendError(env.client, "UNKNOWN_TYPE", "type must be 'message', 'read', or 'typing'")
	}
}

func (h *Hub) handleMessage(sender *Client, in *shortletchat.IncomingWS) {
	if in.ConversationID == "" {
		h.sendError(sender, "MISSING_FIELD", "conversation_id is required")
		return
	}
	if in.Content == "" {
		h.sendError(sender, "MISSING_FIELD", "content is required")
		return
	}

	convoID, err := uuid.Parse(in.ConversationID)
	if err != nil {
		h.sendError(sender, "INVALID_FIELD", "conversation_id must be a valid UUID")
		return
	}

	db := sqlconnect.DB
	if db == nil {
		h.sendError(sender, "INTERNAL", "database unavailable")
		return
	}

	ctx := context.Background()

	// Fetch conversation — verify participant membership and expiry
	var clientID, ownerID uuid.UUID
	var chatExpiresAt *time.Time

	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id, chat_expires_at
		 FROM shortlet_conversations
		 WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&clientID, &ownerID, &chatExpiresAt)
	if err != nil {
		h.sendError(sender, "NOT_FOUND", "conversation not found")
		return
	}

	if sender.UserID != clientID && sender.UserID != ownerID {
		h.sendError(sender, "FORBIDDEN", "you are not a participant of this conversation")
		return
	}

	// Reject writes after the 24 h post-checkout window
	if chatExpiresAt != nil && time.Now().After(*chatExpiresAt) {
		h.sendError(sender, "CONVERSATION_CLOSED",
			"this conversation has been closed 24 hours after checkout")
		return
	}

	// Persist the message
	var msg shortletchat.ShortletMessage
	err = db.QueryRow(ctx, `
		INSERT INTO shortlet_messages (conversation_id, sender_id, sender_role, content, msg_type)
		VALUES ($1, $2, $3, $4, 'text')
		RETURNING id, conversation_id, sender_id, sender_role, content, msg_type, is_read, created_at
	`, convoID, sender.UserID, sender.Role, in.Content).Scan(
		&msg.ID,
		&msg.ConversationID,
		&msg.SenderID,
		&msg.SenderRole,
		&msg.Content,
		&msg.MsgType,
		&msg.IsRead,
		&msg.CreatedAt,
	)
	if err != nil {
		log.Printf("[shortlet-hub] failed to save message: %v", err)
		h.sendError(sender, "INTERNAL", "could not save message")
		return
	}

	payload := buildPayload("message", msg)

	recipientID := clientID
	if sender.UserID == clientID {
		recipientID = ownerID
	}

	h.sendTo(sender.UserID, payload)
	h.sendTo(recipientID, payload)

	if PushNotifier != nil {
		go PushNotifier(recipientID, "New Message", in.Content)
	}
}

func (h *Hub) handleRead(reader *Client, in *shortletchat.IncomingWS) {
	if in.ConversationID == "" {
		h.sendError(reader, "MISSING_FIELD", "conversation_id is required")
		return
	}

	convoID, err := uuid.Parse(in.ConversationID)
	if err != nil {
		h.sendError(reader, "INVALID_FIELD", "conversation_id must be a valid UUID")
		return
	}

	db := sqlconnect.DB
	if db == nil {
		return
	}

	ctx := context.Background()

	_, err = db.Exec(ctx, `
		UPDATE shortlet_messages
		SET    is_read = TRUE
		WHERE  conversation_id = $1
		  AND  sender_id <> $2
		  AND  is_read = FALSE
	`, convoID, reader.UserID)
	if err != nil {
		log.Printf("[shortlet-hub] mark read error: %v", err)
		return
	}

	var clientID, ownerID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id FROM shortlet_conversations WHERE id = $1`, convoID,
	).Scan(&clientID, &ownerID)
	if err != nil {
		return
	}

	otherID := clientID
	if reader.UserID == clientID {
		otherID = ownerID
	}

	payload := buildPayload("read", shortletchat.ReadPayload{
		ConversationID: in.ConversationID,
		ReaderID:       reader.UserID,
	})
	h.sendTo(otherID, payload)
}

func (h *Hub) handleTyping(typer *Client, in *shortletchat.IncomingWS) {
	if in.ConversationID == "" {
		return
	}

	convoID, err := uuid.Parse(in.ConversationID)
	if err != nil {
		return
	}

	db := sqlconnect.DB
	if db == nil {
		return
	}

	ctx := context.Background()

	var clientID, ownerID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id FROM shortlet_conversations WHERE id = $1`, convoID,
	).Scan(&clientID, &ownerID)
	if err != nil {
		return
	}

	otherID := clientID
	if typer.UserID == clientID {
		otherID = ownerID
	}

	payload := buildPayload("typing", shortletchat.TypingPayload{
		ConversationID: in.ConversationID,
		SenderID:       typer.UserID,
	})
	h.sendTo(otherID, payload)
}

// ============================================================================
// Delivery helpers
// ============================================================================

func (h *Hub) sendTo(userID uuid.UUID, payload []byte) {
	h.mu.RLock()
	c, ok := h.clients[userID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case c.send <- payload:
	default:
		// Slow consumer — drop and evict
		h.mu.Lock()
		delete(h.clients, userID)
		h.mu.Unlock()
		close(c.send)
	}
}

func (h *Hub) sendError(c *Client, code, msg string) {
	payload := buildPayload("error", shortletchat.ErrorPayload{Code: code, Message: msg})
	select {
	case c.send <- payload:
	default:
	}
}

func buildPayload(msgType string, payload interface{}) []byte {
	b, _ := json.Marshal(shortletchat.OutgoingWS{Type: msgType, Payload: payload})
	return b
}
