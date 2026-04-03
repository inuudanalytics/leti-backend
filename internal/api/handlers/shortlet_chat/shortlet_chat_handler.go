package shortletchathandler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"leti_server/internal/api/handlers"
	shortletchat "leti_server/internal/models/shortlet_chat"
	"leti_server/internal/repositories/sqlconnect"
	shortlethub "leti_server/internal/shortlethub"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var Hub *shortlethub.Hub

// ============================================================================
// WS  GET /shortlet-chat/ws
// ============================================================================

// ServeWS godoc
// @Summary      Connect to the shortlet chat WebSocket
// @Description  Upgrades the HTTP connection to WebSocket for shortlet (property) chat.
// @Description  Participants are the client who booked and the property owner.
// @Description
// @Description  **Incoming frames (client → server):**
// @Description  Send text: `{"type":"message","conversation_id":"<uuid>","content":"Hello!"}`
// @Description  Mark read: `{"type":"read","conversation_id":"<uuid>"}`
// @Description  Typing:    `{"type":"typing","conversation_id":"<uuid>"}`
// @Description
// @Description  **Outgoing frames (server → client):**
// @Description  New message: `{"type":"message","payload":{...ShortletMessage}}`
// @Description  Read receipt:`{"type":"read","payload":{"conversation_id":"...","reader_id":"..."}}`
// @Description  Typing:      `{"type":"typing","payload":{"conversation_id":"...","sender_id":"..."}}`
// @Description  Error:       `{"type":"error","payload":{"code":"...","message":"..."}}`
// @Tags         Shortlet Chat
// @Success      101  {string}  string  "Switching Protocols"
// @Failure      401  {object}  object{error=string}
// @Router       /shortlet-chat/ws [get]
// @Security     BearerAuth
func ServeWS(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	role, ok := r.Context().Value(utils.ContextKey("role")).(string)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.Logger.Errorf("shortlet ws upgrade failed for user %s: %v", userID, err)
		return
	}

	client := Hub.NewClient(conn, userID, role)
	go client.WritePump()
	go client.ReadPump()
}

// ============================================================================
// POST /shortlet-chat/conversations
// ============================================================================

// StartShortletConversation godoc
// @Summary      Start or retrieve a shortlet conversation
// @Description  Creates (or retrieves) a conversation between the client and property owner for a confirmed order.
// @Description  Only the client who owns the order can start the conversation.
// @Description  The order must be in confirmed, checked_in, or completed status.
// @Tags         Shortlet Chat
// @Accept       json
// @Produce      json
// @Param        body  body  object{order_id=string}  true  "Order UUID"
// @Success      200  {object}  object{status=string,message=string,conversation=shortletchat.ShortletConversation}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /shortlet-chat/conversations [post]
// @Security     BearerAuth
func StartShortletConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	role, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if role != "client" {
		utils.WriteError(w, "only clients can start a shortlet conversation", http.StatusForbidden)
		return
	}

	type request struct {
		OrderID string `json:"order_id"`
	}

	var req request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.OrderID == "" {
		utils.WriteError(w, "order_id is required", http.StatusBadRequest)
		return
	}

	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		utils.WriteError(w, "invalid order_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var ownerID uuid.UUID
	var orderStatus string
	var checkoutDate string

	err = db.QueryRow(ctx, `
		SELECT owner_id, status, check_out_date::TEXT
		FROM orders
		WHERE id = $1 AND client_id = $2
	`, orderID, userID).Scan(&ownerID, &orderStatus, &checkoutDate)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found or you do not own it", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("shortlet chat: failed to fetch order: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	switch orderStatus {
	case "confirmed", "checked_in", "completed":
	default:
		utils.WriteError(w,
			"chat is only available for confirmed, checked_in, or completed orders",
			http.StatusForbidden)
		return
	}

	var convo shortletchat.ShortletConversation
	var xmax uint64

	err = db.QueryRow(ctx, `
		INSERT INTO shortlet_conversations (client_id, owner_id, order_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (order_id) DO UPDATE
			SET updated_at = shortlet_conversations.updated_at
		RETURNING id, client_id, owner_id, order_id,
		          chat_expires_at, deleted_at, created_at, updated_at, xmax
	`, userID, ownerID, orderID).Scan(
		&convo.ID,
		&convo.ClientID,
		&convo.OwnerID,
		&convo.OrderID,
		&convo.ChatExpiresAt,
		&convo.DeletedAt,
		&convo.CreatedAt,
		&convo.UpdatedAt,
		&xmax,
	)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: upsert conversation failed: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	isNew := xmax == 0
	msg := "conversation retrieved"
	if isNew {
		msg = "conversation started"
		if Hub != nil && shortlethub.PushNotifier != nil {
			go shortlethub.PushNotifier(ownerID,
				"New Guest Message",
				"A guest has opened a conversation for their booking.")
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":       "success",
		"message":      msg,
		"conversation": convo,
	})
}

// ============================================================================
// GET /shortlet-chat/conversations
// ============================================================================

// GetMyShortletConversations godoc
// @Summary      List shortlet conversations
// @Description  Returns a paginated list of shortlet conversations for the authenticated user (client or owner), with last-message preview and unread count.
// @Tags         Shortlet Chat
// @Produce      json
// @Param        page   query  int  false  "Page number (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]object,pagination=object}
// @Failure      401  {object}  object{error=string}
// @Router       /shortlet-chat/conversations [get]
// @Security     BearerAuth
func GetMyShortletConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	var totalCount int
	err := db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM shortlet_conversations
		WHERE (client_id = $1 OR owner_id = $1)
		  AND deleted_at IS NULL
	`, userID).Scan(&totalCount)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: count conversations: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT
			c.id,
			c.client_id,
			(cu.first_name || ' ' || cu.last_name) AS client_name,
			c.owner_id,
			(ou.first_name || ' ' || ou.last_name)  AS owner_name,
			c.order_id,
			c.chat_expires_at,
			c.updated_at,
			lm.content      AS last_message_content,
			lm.msg_type     AS last_message_type,
			lm.created_at   AS last_message_at,
			COUNT(unread.id) AS unread_count
		FROM shortlet_conversations c
		JOIN users cu ON cu.id = c.client_id
		JOIN users ou ON ou.id = c.owner_id

		LEFT JOIN LATERAL (
			SELECT content, msg_type, created_at
			FROM   shortlet_messages
			WHERE  conversation_id = c.id
			ORDER  BY created_at DESC
			LIMIT  1
		) lm ON true

		LEFT JOIN shortlet_messages unread
			ON  unread.conversation_id = c.id
			AND unread.sender_id      <> $1
			AND unread.is_read         = FALSE

		WHERE (c.client_id = $1 OR c.owner_id = $1)
		  AND c.deleted_at IS NULL
		GROUP BY c.id,
		         cu.first_name, cu.last_name,
		         ou.first_name, ou.last_name,
		         lm.content, lm.msg_type, lm.created_at
		ORDER BY c.updated_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: fetch conversations: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ParticipantInfo struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}

	type LastMessage struct {
		Content   string    `json:"content"`
		MsgType   string    `json:"msg_type"`
		CreatedAt time.Time `json:"created_at"`
	}

	type ConversationItem struct {
		ID            uuid.UUID       `json:"id"`
		Client        ParticipantInfo `json:"client"`
		Owner         ParticipantInfo `json:"owner"`
		OrderID       uuid.UUID       `json:"order_id"`
		ChatExpiresAt *time.Time      `json:"chat_expires_at,omitempty"`
		LastMessage   *LastMessage    `json:"last_message"`
		UnreadCount   int             `json:"unread_count"`
		UpdatedAt     time.Time       `json:"updated_at"`
	}

	conversations := make([]ConversationItem, 0)
	for rows.Next() {
		var (
			item        ConversationItem
			clientID    uuid.UUID
			clientName  string
			ownerID     uuid.UUID
			ownerName   string
			lastContent *string
			lastType    *string
			lastAt      *time.Time
			unread      int
		)

		if err := rows.Scan(
			&item.ID,
			&clientID, &clientName,
			&ownerID, &ownerName,
			&item.OrderID,
			&item.ChatExpiresAt,
			&item.UpdatedAt,
			&lastContent, &lastType, &lastAt,
			&unread,
		); err != nil {
			utils.Logger.Errorf("shortlet chat: scan conversation: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		item.Client = ParticipantInfo{ID: clientID, Name: clientName}
		item.Owner = ParticipantInfo{ID: ownerID, Name: ownerName}
		item.UnreadCount = unread

		if lastContent != nil && lastType != nil && lastAt != nil {
			preview := *lastContent
			if *lastType == "image" {
				preview = "📷 Image"
			}
			item.LastMessage = &LastMessage{
				Content:   preview,
				MsgType:   *lastType,
				CreatedAt: *lastAt,
			}
		}

		conversations = append(conversations, item)
	}
	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("shortlet chat: row error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	totalPages := (totalCount + limit - 1) / limit

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(conversations),
		"data":   conversations,
		"pagination": map[string]int{
			"total":       totalCount,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /shortlet-chat/conversations/{id}/messages
// ============================================================================

// GetShortletMessages godoc
// @Summary      Get shortlet message history
// @Description  Returns paginated messages for a shortlet conversation. Caller must be a participant.
// @Tags         Shortlet Chat
// @Produce      json
// @Param        id      path   string  true   "Conversation UUID"
// @Param        limit   query  int     false  "Max messages (default 50, max 100)"
// @Param        offset  query  int     false  "Offset (default 0)"
// @Success      200  {object}  object{status=string,count=int,data=[]shortletchat.ShortletMessage,pagination=object}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /shortlet-chat/conversations/{id}/messages [get]
// @Security     BearerAuth
func GetShortletMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	convoID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid conversation id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var clientID, ownerID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id FROM shortlet_conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&clientID, &ownerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != clientID && userID != ownerID {
		utils.WriteError(w, "you are not a participant of this conversation", http.StatusForbidden)
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	var totalCount int
	db.QueryRow(ctx,
		`SELECT COUNT(*) FROM shortlet_messages WHERE conversation_id = $1`, convoID,
	).Scan(&totalCount)

	rows, err := db.Query(ctx, `
		SELECT id, conversation_id, sender_id, sender_role,
		       content, msg_type, is_read, created_at
		FROM   shortlet_messages
		WHERE  conversation_id = $1
		ORDER  BY created_at ASC
		LIMIT  $2 OFFSET $3
	`, convoID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: fetch messages: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]shortletchat.ShortletMessage, 0)
	for rows.Next() {
		var msg shortletchat.ShortletMessage
		if err := rows.Scan(
			&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.SenderRole,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		); err != nil {
			utils.Logger.Errorf("shortlet chat: scan message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		messages = append(messages, msg)
	}

	totalPages := (totalCount + limit - 1) / limit

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(messages),
		"data":   messages,
		"pagination": map[string]int{
			"total":       totalCount,
			"limit":       limit,
			"offset":      offset,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// PATCH /shortlet-chat/conversations/{id}/read
// ============================================================================

// MarkShortletMessagesRead godoc
// @Summary      Mark shortlet messages as read
// @Description  Marks all unread messages sent by the other party as read and notifies them via WebSocket.
// @Tags         Shortlet Chat
// @Produce      json
// @Param        id  path  string  true  "Conversation UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /shortlet-chat/conversations/{id}/read [patch]
// @Security     BearerAuth
func MarkShortletMessagesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	convoID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid conversation id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var clientID, ownerID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id FROM shortlet_conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&clientID, &ownerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != clientID && userID != ownerID {
		utils.WriteError(w, "you are not a participant of this conversation", http.StatusForbidden)
		return
	}

	_, err = db.Exec(ctx, `
		UPDATE shortlet_messages
		SET    is_read = TRUE
		WHERE  conversation_id = $1
		  AND  sender_id <> $2
		  AND  is_read = FALSE
	`, convoID, userID)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: mark read: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if Hub != nil {
		otherID := clientID
		if userID == clientID {
			otherID = ownerID
		}
		payload, _ := json.Marshal(shortletchat.OutgoingWS{
			Type: "read",
			Payload: shortletchat.ReadPayload{
				ConversationID: convoID.String(),
				ReaderID:       userID,
			},
		})
		Hub.DeliverTo(otherID, payload)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "messages marked as read",
	})
}

// ============================================================================
// POST /shortlet-chat/conversations/{id}/images
// ============================================================================

// SendShortletImageMessage godoc
// @Summary      Send image message(s) in a shortlet conversation
// @Description  Uploads 1–5 images to Cloudinary and inserts each as an image message. Both participants receive a WebSocket push per image.
// @Tags         Shortlet Chat
// @Accept       mpfd
// @Produce      json
// @Param        id      path      string  true  "Conversation UUID"
// @Param        images  formData  file    true  "Image file(s) — max 5"
// @Success      200  {object}  object{status=string,message=string,messages=[]shortletchat.ShortletMessage}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /shortlet-chat/conversations/{id}/images [post]
// @Security     BearerAuth
func SendShortletImageMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	role, _ := r.Context().Value(utils.ContextKey("role")).(string)

	convoID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid conversation id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var clientID, ownerID uuid.UUID
	var chatExpiresAt *time.Time
	err = db.QueryRow(ctx,
		`SELECT client_id, owner_id, chat_expires_at
		 FROM shortlet_conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&clientID, &ownerID, &chatExpiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != clientID && userID != ownerID {
		utils.WriteError(w, "you are not a participant of this conversation", http.StatusForbidden)
		return
	}

	if chatExpiresAt != nil && time.Now().After(*chatExpiresAt) {
		utils.WriteError(w,
			"this conversation has been closed 24 hours after checkout",
			http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	imageHeaders := r.MultipartForm.File["images"]
	if len(imageHeaders) == 0 {
		utils.WriteError(w, "at least one image is required", http.StatusBadRequest)
		return
	}
	if len(imageHeaders) > 5 {
		utils.WriteError(w, "a maximum of 5 images can be sent at once", http.StatusBadRequest)
		return
	}

	var files []utils.UploadFile
	var openFiles []io.Closer
	for _, h := range imageHeaders {
		file, err := h.Open()
		if err != nil {
			continue
		}
		files = append(files, utils.UploadFile{Reader: file, Filename: h.Filename})
		openFiles = append(openFiles, file)
	}
	defer func() {
		for _, f := range openFiles {
			f.Close()
		}
	}()

	cloud, err := utils.InitCloudinary()
	if err != nil {
		utils.WriteError(w, "failed to initialize image service", http.StatusInternalServerError)
		return
	}

	urls, _, err := handlers.UploadFilesConcurrently(ctx, cloud, files, "shortlet-chat/images")
	if err != nil || len(urls) == 0 {
		utils.Logger.Errorf("shortlet chat: image upload failed: %v", err)
		utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
		return
	}

	recipientID := clientID
	if userID == clientID {
		recipientID = ownerID
	}

	var savedMessages []shortletchat.ShortletMessage
	for _, url := range urls {
		var msg shortletchat.ShortletMessage
		err = db.QueryRow(ctx, `
			INSERT INTO shortlet_messages (conversation_id, sender_id, sender_role, content, msg_type)
			VALUES ($1, $2, $3, $4, 'image')
			RETURNING id, conversation_id, sender_id, sender_role, content, msg_type, is_read, created_at
		`, convoID, userID, role, url).Scan(
			&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.SenderRole,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		)
		if err != nil {
			utils.Logger.Errorf("shortlet chat: insert image message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if Hub != nil {
			payload, _ := json.Marshal(shortletchat.OutgoingWS{Type: "message", Payload: msg})
			Hub.DeliverTo(userID, payload)
			Hub.DeliverTo(recipientID, payload)
		}

		savedMessages = append(savedMessages, msg)
	}

	if shortlethub.PushNotifier != nil {
		go shortlethub.PushNotifier(recipientID,
			"New Image",
			fmt.Sprintf("%d image(s) sent", len(savedMessages)))
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  fmt.Sprintf("%d image(s) sent", len(savedMessages)),
		"messages": savedMessages,
	})
}

// ============================================================================
// DELETE /shortlet-chat/conversations/{id}
// ============================================================================

// DeleteShortletConversation godoc
// @Summary      Delete a shortlet conversation
// @Description  Soft-deletes a shortlet conversation. Only the client who created it can delete it.
// @Tags         Shortlet Chat
// @Produce      json
// @Param        id  path  string  true  "Conversation UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      404  {object}  object{error=string}
// @Router       /shortlet-chat/conversations/{id} [delete]
// @Security     BearerAuth
func DeleteShortletConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	convoID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid conversation id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := db.Exec(ctx, `
		UPDATE shortlet_conversations
		SET    deleted_at = NOW()
		WHERE  id = $1
		  AND  client_id = $2
		  AND  deleted_at IS NULL
	`, convoID, userID)
	if err != nil {
		utils.Logger.Errorf("shortlet chat: delete conversation: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w,
			"conversation not found or you do not have permission to delete it",
			http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "conversation deleted",
	})
}
