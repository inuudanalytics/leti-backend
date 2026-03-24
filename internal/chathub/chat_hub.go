package chathub

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	chatModels "leti_server/internal/models/chat"
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

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	UserID uuid.UUID
	Role   string
}

// ReadPump pumps messages from the WebSocket connection to the hub.
// Runs in its own goroutine per client.
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
				log.Printf("[chat-hub] read error user %s: %v", c.UserID, err)
			}
			break
		}
		c.hub.inbound <- &inboundEnvelope{client: c, raw: raw}
	}
}

// WritePump pumps messages from the hub send channel to the WebSocket connection.
// Runs in its own goroutine per client.
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

type Hub struct {
	mu         sync.RWMutex
	clients    map[uuid.UUID]*Client
	register   chan *Client
	unregister chan *Client
	inbound    chan *inboundEnvelope
}

// New creates a Hub. Call go hub.Run() after creation.
func New() *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]*Client),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		inbound:    make(chan *inboundEnvelope, 256),
	}
}

// NewClient creates a Client and queues it for registration with the hub.
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

// Run is the hub's main event loop. Call as a goroutine — blocks forever.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.UserID] = c
			h.mu.Unlock()
			log.Printf("[chat-hub] connected: %s (%s)", c.UserID, c.Role)

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.UserID]; ok {
				delete(h.clients, c.UserID)
				close(c.send)
			}
			h.mu.Unlock()
			log.Printf("[chat-hub] disconnected: %s", c.UserID)

		case env := <-h.inbound:
			h.handleInbound(env)
		}
	}
}

// DeliverTo pushes an already-serialised payload to a connected user.
// No-op if the user is offline (message is already persisted in DB).
func (h *Hub) DeliverTo(userID uuid.UUID, payload []byte) {
	h.sendTo(userID, payload)
}

// ============================================================================
// Push notification hook — set in main after hub initialisation
// ============================================================================

var PushNotifier func(userID uuid.UUID, title, body string)

// ============================================================================
// Inbound routing
// ============================================================================

func (h *Hub) handleInbound(env *inboundEnvelope) {
	var in chatModels.IncomingWS
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

func (h *Hub) handleMessage(sender *Client, in *chatModels.IncomingWS) {
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

	// Verify sender belongs to this conversation
	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		h.sendError(sender, "NOT_FOUND", "conversation not found")
		return
	}

	if sender.UserID != ownerID && sender.UserID != artisanID {
		h.sendError(sender, "FORBIDDEN", "you are not a participant of this conversation")
		return
	}

	// Persist message to DB
	var msg chatModels.Message
	err = db.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_id, sender_role, content, msg_type)
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
		log.Printf("[chat-hub] failed to save message: %v", err)
		h.sendError(sender, "INTERNAL", "could not save message")
		return
	}

	payload := buildPayload("message", msg)

	// Determine recipient
	recipientID := ownerID
	if sender.UserID == ownerID {
		recipientID = artisanID
	}

	// Ack to sender and deliver to recipient
	h.sendTo(sender.UserID, payload)
	h.sendTo(recipientID, payload)

	// Push notification to recipient (non-blocking, only if offline)
	if PushNotifier != nil {
		go PushNotifier(recipientID, "New Message", in.Content)
	}
}

func (h *Hub) handleRead(reader *Client, in *chatModels.IncomingWS) {
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
		UPDATE messages
		SET    is_read = TRUE
		WHERE  conversation_id = $1
		  AND  sender_id <> $2
		  AND  is_read = FALSE
	`, convoID, reader.UserID)
	if err != nil {
		log.Printf("[chat-hub] mark read error: %v", err)
		return
	}

	// Notify the other party their messages were read
	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1`, convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		return
	}

	otherID := ownerID
	if reader.UserID == ownerID {
		otherID = artisanID
	}

	payload := buildPayload("read", chatModels.ReadPayload{
		ConversationID: in.ConversationID,
		ReaderID:       reader.UserID,
	})
	h.sendTo(otherID, payload)
}

func (h *Hub) handleTyping(typer *Client, in *chatModels.IncomingWS) {
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

	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1`, convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		return
	}

	otherID := ownerID
	if typer.UserID == ownerID {
		otherID = artisanID
	}

	payload := buildPayload("typing", chatModels.TypingPayload{
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
		return // offline — message is persisted, fetched on reconnect via REST
	}

	select {
	case c.send <- payload:
	default:
		// Slow / dead client — evict
		h.mu.Lock()
		delete(h.clients, userID)
		h.mu.Unlock()
		close(c.send)
	}
}

func (h *Hub) sendError(c *Client, code, msg string) {
	payload := buildPayload("error", chatModels.ErrorPayload{Code: code, Message: msg})
	select {
	case c.send <- payload:
	default:
	}
}

func buildPayload(msgType string, payload interface{}) []byte {
	b, _ := json.Marshal(chatModels.OutgoingWS{Type: msgType, Payload: payload})
	return b
}
