package supporthub

import (
	"context"
	"encoding/json"
	supportModels "leti_server/internal/models/support"
	"leti_server/internal/repositories/sqlconnect"
	"log"
	"sync"
	"time"

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
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	UserID     uuid.UUID
	SenderType string // "user" | "admin"
}

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
				log.Printf("[support-hub] read error user %s: %v", c.UserID, err)
			}
			break
		}
		c.hub.inbound <- &inboundEnvelope{client: c, raw: raw}
	}
}

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

// PushNotifier is set from main after hub init
var PushNotifier func(userID uuid.UUID, title, body string, data ...map[string]string)

func New() *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]*Client),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		inbound:    make(chan *inboundEnvelope, 256),
	}
}

func (h *Hub) NewClient(conn *websocket.Conn, userID uuid.UUID, senderType string) *Client {
	c := &Client{
		hub:        h,
		conn:       conn,
		send:       make(chan []byte, 256),
		UserID:     userID,
		SenderType: senderType,
	}
	h.register <- c
	return c
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.UserID] = c
			h.mu.Unlock()
			log.Printf("[support-hub] connected: %s (%s)", c.UserID, c.SenderType)

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.UserID]; ok {
				delete(h.clients, c.UserID)
				close(c.send)
			}
			h.mu.Unlock()
			log.Printf("[support-hub] disconnected: %s", c.UserID)

		case env := <-h.inbound:
			h.handleInbound(env)
		}
	}
}

func (h *Hub) DeliverTo(userID uuid.UUID, payload []byte) {
	h.sendTo(userID, payload)
}

// ============================================================================
// Inbound routing
// ============================================================================

func (h *Hub) handleInbound(env *inboundEnvelope) {
	var in supportModels.SupportWSIncoming
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

func (h *Hub) handleMessage(sender *Client, in *supportModels.SupportWSIncoming) {
	if in.TicketID == "" {
		h.sendError(sender, "MISSING_FIELD", "ticket_id is required")
		return
	}
	if in.Content == "" {
		h.sendError(sender, "MISSING_FIELD", "content is required")
		return
	}

	ticketID, err := uuid.Parse(in.TicketID)
	if err != nil {
		h.sendError(sender, "INVALID_FIELD", "ticket_id must be a valid UUID")
		return
	}

	db := sqlconnect.DB
	if db == nil {
		h.sendError(sender, "INTERNAL", "database unavailable")
		return
	}

	ctx := context.Background()

	var ticketUserID uuid.UUID
	var assignedAdminID *uuid.UUID
	var ticketStatus string

	err = db.QueryRow(ctx, `
		SELECT user_id, assigned_admin_id, status
		FROM support_tickets
		WHERE id = $1
	`, ticketID).Scan(&ticketUserID, &assignedAdminID, &ticketStatus)
	if err != nil {
		h.sendError(sender, "NOT_FOUND", "ticket not found")
		return
	}

	if ticketStatus == "closed" || ticketStatus == "resolved" {
		h.sendError(sender, "FORBIDDEN", "ticket is closed")
		return
	}

	// Any platform user who owns the ticket OR any admin can message
	isTicketOwner := sender.SenderType == "user" && sender.UserID == ticketUserID
	isAdmin := sender.SenderType == "admin"

	if !isTicketOwner && !isAdmin {
		h.sendError(sender, "FORBIDDEN", "you are not a participant of this ticket")
		return
	}

	// Auto-assign admin on first message if not yet assigned
	if isAdmin && assignedAdminID == nil {
		_, err = db.Exec(ctx, `
			UPDATE support_tickets
			SET assigned_admin_id = $1,
			    status = CASE WHEN status = 'open' THEN 'in_progress' ELSE status END,
			    updated_at = NOW()
			WHERE id = $2
		`, sender.UserID, ticketID)
		if err != nil {
			log.Printf("[support-hub] failed to auto-assign admin: %v", err)
		}
		assignedAdminID = &sender.UserID
	}

	var msg supportModels.SupportMessage
	err = db.QueryRow(ctx, `
		INSERT INTO support_messages (ticket_id, sender_id, sender_type, content, msg_type)
		VALUES ($1, $2, $3, $4, 'text')
		RETURNING id, ticket_id, sender_id, sender_type, content, msg_type, is_read, created_at
	`, ticketID, sender.UserID, sender.SenderType, in.Content).Scan(
		&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderType,
		&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
	)
	if err != nil {
		log.Printf("[support-hub] failed to save message: %v", err)
		h.sendError(sender, "INTERNAL", "could not save message")
		return
	}

	payload := buildPayload("message", msg)

	if isAdmin {
		// Deliver ack to admin, deliver to ticket owner (client/artisan/owner)
		h.sendTo(sender.UserID, payload)
		h.sendTo(ticketUserID, payload)
		if PushNotifier != nil {
			go PushNotifier(ticketUserID, "Support Reply", in.Content)
		}
	} else {
		// Deliver ack to user, deliver to assigned admin (or broadcast)
		h.sendTo(sender.UserID, payload)
		if assignedAdminID != nil {
			h.sendTo(*assignedAdminID, payload)
			if PushNotifier != nil {
				go PushNotifier(*assignedAdminID, "Support Message", in.Content)
			}
		} else {
			// No admin assigned yet — broadcast to all connected admins
			h.mu.RLock()
			for _, c := range h.clients {
				if c.SenderType == "admin" {
					select {
					case c.send <- payload:
					default:
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) handleRead(reader *Client, in *supportModels.SupportWSIncoming) {
	if in.TicketID == "" {
		return
	}
	ticketID, err := uuid.Parse(in.TicketID)
	if err != nil {
		return
	}

	db := sqlconnect.DB
	if db == nil {
		return
	}

	ctx := context.Background()

	_, err = db.Exec(ctx, `
		UPDATE support_messages
		SET is_read = TRUE
		WHERE ticket_id = $1 AND sender_id <> $2 AND is_read = FALSE
	`, ticketID, reader.UserID)
	if err != nil {
		log.Printf("[support-hub] mark read error: %v", err)
	}

	var ticketUserID uuid.UUID
	var assignedAdminID *uuid.UUID
	db.QueryRow(ctx, `
		SELECT user_id, assigned_admin_id FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ticketUserID, &assignedAdminID)

	var otherID uuid.UUID
	if reader.SenderType == "user" && assignedAdminID != nil {
		otherID = *assignedAdminID
	} else {
		otherID = ticketUserID
	}

	payload := buildPayload("read", map[string]interface{}{
		"ticket_id": in.TicketID,
		"reader_id": reader.UserID,
	})
	h.sendTo(otherID, payload)
}

func (h *Hub) handleTyping(typer *Client, in *supportModels.SupportWSIncoming) {
	if in.TicketID == "" {
		return
	}
	ticketID, err := uuid.Parse(in.TicketID)
	if err != nil {
		return
	}

	db := sqlconnect.DB
	if db == nil {
		return
	}

	ctx := context.Background()

	var ticketUserID uuid.UUID
	var assignedAdminID *uuid.UUID
	db.QueryRow(ctx, `
		SELECT user_id, assigned_admin_id FROM support_tickets WHERE id = $1
	`, ticketID).Scan(&ticketUserID, &assignedAdminID)

	var otherID uuid.UUID
	if typer.SenderType == "user" && assignedAdminID != nil {
		otherID = *assignedAdminID
	} else {
		otherID = ticketUserID
	}

	payload := buildPayload("typing", map[string]interface{}{
		"ticket_id": in.TicketID,
		"sender_id": typer.UserID,
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
		h.mu.Lock()
		delete(h.clients, userID)
		h.mu.Unlock()
		close(c.send)
	}
}

func (h *Hub) sendError(c *Client, code, msg string) {
	payload := buildPayload("error", map[string]string{"code": code, "message": msg})
	select {
	case c.send <- payload:
	default:
	}
}

func buildPayload(msgType string, payload interface{}) []byte {
	type outgoing struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}
	b, _ := json.Marshal(outgoing{Type: msgType, Payload: payload})
	return b
}
