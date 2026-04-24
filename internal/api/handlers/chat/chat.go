package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"leti_server/internal/api/handlers"
	chathub "leti_server/internal/chathub"
	"leti_server/internal/models/chat"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var Hub *chathub.Hub

// ============================================================================
// WS  GET /chat/ws
// ============================================================================

// ServeWS godoc
// @Summary      Connect to the chat WebSocket
// @Description  Upgrades the connection to a WebSocket. The authenticated user is registered with the chat hub. All chat messaging, typing indicators, and read receipts happen over this connection.
// @Description
// @Description  **Incoming frames (client → server):**
// @Description
// @Description  Send a text message:
// @Description  `{"type":"message","conversation_id":"<uuid>","content":"Hello!"}`
// @Description
// @Description  Mark messages as read:
// @Description  `{"type":"read","conversation_id":"<uuid>"}`
// @Description
// @Description  Typing indicator:
// @Description  `{"type":"typing","conversation_id":"<uuid>"}`
// @Description
// @Description  **Outgoing frames (server → client):**
// @Description
// @Description  New message: `{"type":"message","payload":{...Message}}`
// @Description  Read receipt: `{"type":"read","payload":{"conversation_id":"...","reader_id":"..."}}`
// @Description  Typing: `{"type":"typing","payload":{"conversation_id":"...","sender_id":"..."}}`
// @Description  Error: `{"type":"error","payload":{"code":"...","message":"..."}}`
// @Tags         Chat
// @Success      101  {string}  string  "Switching Protocols"
// @Failure      401  {object}  object{error=string}
// @Router       /chat/ws [get]
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
		utils.Logger.Errorf("websocket upgrade failed for user %s: %v", userID, err)
		return
	}

	client := Hub.NewClient(conn, userID, role)
	go client.WritePump()
	go client.ReadPump()
}

// ============================================================================
// POST /chat/conversations
// ============================================================================

// StartConversation godoc
// @Summary      Start or retrieve a conversation
// @Description  Creates a new conversation between a client and an artisan for a specific job OR artisan booking, or retrieves the existing one. Only the client who owns the job/booking can start the conversation. Provide exactly one of job_id or booking_id.
// @Tags         Chat
// @Accept       json
// @Produce      json
// @Param        body  body  object{artisan_id=string,job_id=string,booking_id=string}  true  "Artisan UUID plus exactly one of job_id or booking_id"
// @Success      200   {object}  object{status=string,message=string,conversation=chat.Conversation}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /chat/conversations [post]
// @Security     BearerAuth
func StartConversation(w http.ResponseWriter, r *http.Request) {
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
		utils.WriteError(w, "only clients can start a conversation", http.StatusForbidden)
		return
	}

	type request struct {
		ArtisanID string `json:"artisan_id"`
		JobID     string `json:"job_id,omitempty"`
		BookingID string `json:"booking_id,omitempty"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ArtisanID == "" {
		utils.WriteError(w, "artisan_id is required", http.StatusBadRequest)
		return
	}

	if req.JobID == "" && req.BookingID == "" {
		utils.WriteError(w, "one of job_id or booking_id is required", http.StatusBadRequest)
		return
	}
	if req.JobID != "" && req.BookingID != "" {
		utils.WriteError(w, "provide either job_id or booking_id, not both", http.StatusBadRequest)
		return
	}

	artisanID, err := uuid.Parse(req.ArtisanID)
	if err != nil {
		utils.WriteError(w, "invalid artisan_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var convo chat.Conversation
	var xmax uint64
	var statusMsg string

	if req.JobID != "" {
		jobID, err := uuid.Parse(req.JobID)
		if err != nil {
			utils.WriteError(w, "invalid job_id", http.StatusBadRequest)
			return
		}

		var jobClientID uuid.UUID
		err = db.QueryRow(ctx,
			`SELECT client_id FROM jobs WHERE id = $1`,
			jobID,
		).Scan(&jobClientID)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "job not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("failed to fetch job: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if jobClientID != userID {
			utils.WriteError(w, "you do not own this job", http.StatusForbidden)
			return
		}

		err = db.QueryRow(ctx, `
			INSERT INTO conversations (owner_id, artisan_id, job_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (job_id) WHERE job_id IS NOT NULL DO UPDATE
				SET updated_at = conversations.updated_at
			RETURNING id, owner_id, artisan_id, job_id, booking_id, created_at, updated_at, xmax
		`, userID, artisanID, jobID).Scan(
			&convo.ID,
			&convo.OwnerID,
			&convo.ArtisanID,
			&convo.JobID,
			&convo.BookingID,
			&convo.CreatedAt,
			&convo.UpdatedAt,
			&xmax,
		)
		if err != nil {
			utils.Logger.Errorf("failed to upsert job conversation: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		statusMsg = "conversation retrieved"
		if xmax == 0 {
			statusMsg = "conversation started"
			if Hub != nil && chathub.PushNotifier != nil {
				go chathub.PushNotifier(artisanID, "New Job Request", "A client has opened a conversation for a job.")
			}
		}
	}

	if req.BookingID != "" {
		bookingID, err := uuid.Parse(req.BookingID)
		if err != nil {
			utils.WriteError(w, "invalid booking_id", http.StatusBadRequest)
			return
		}

		var bookingClientID uuid.UUID
		err = db.QueryRow(ctx,
			`SELECT client_id FROM artisan_bookings WHERE id = $1`,
			bookingID,
		).Scan(&bookingClientID)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "booking not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("failed to fetch booking: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if bookingClientID != userID {
			utils.WriteError(w, "you do not own this booking", http.StatusForbidden)
			return
		}

		err = db.QueryRow(ctx, `
			INSERT INTO conversations (owner_id, artisan_id, booking_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (booking_id) WHERE booking_id IS NOT NULL DO UPDATE
				SET updated_at = conversations.updated_at
			RETURNING id, owner_id, artisan_id, job_id, booking_id, created_at, updated_at, xmax
		`, userID, artisanID, bookingID).Scan(
			&convo.ID,
			&convo.OwnerID,
			&convo.ArtisanID,
			&convo.JobID,
			&convo.BookingID,
			&convo.CreatedAt,
			&convo.UpdatedAt,
			&xmax,
		)
		if err != nil {
			utils.Logger.Errorf("failed to upsert booking conversation: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		statusMsg = "conversation retrieved"
		if xmax == 0 {
			statusMsg = "conversation started"
			if Hub != nil && chathub.PushNotifier != nil {
				go chathub.PushNotifier(artisanID, "New Booking", "A client has opened a conversation for a booking.")
			}
		}
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":       "success",
		"message":      statusMsg,
		"conversation": convo,
	})
}

// ============================================================================
// GET /chat/conversations
// ============================================================================

// GetMyConversations godoc
// @Summary      List conversations
// @Description  Returns a paginated list of all conversations for the authenticated user, with the last message preview and unread count per conversation.
// @Tags         Chat
// @Produce      json
// @Param        page   query  int  false  "Page number (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=[]object{id=string,owner=object{id=string,name=string},artisan=object{id=string,name=string},last_message=object{content=string,msg_type=string,created_at=string},unread_count=int,updated_at=string},pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      401  {object}  object{error=string}
// @Router       /chat/conversations [get]
// @Security     BearerAuth
func GetMyConversations(w http.ResponseWriter, r *http.Request) {
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
		SELECT COUNT(*) FROM conversations
		WHERE (owner_id = $1 OR artisan_id = $1)
		  AND deleted_at IS NULL
	`, userID).Scan(&totalCount)
	if err != nil {
		utils.Logger.Errorf("failed to count conversations: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT
			c.id,
			c.owner_id,
			owner.username AS owner_name,
			artisan.username AS artisan_name,
			c.updated_at,
			lm.content        AS last_message_content,
			lm.msg_type       AS last_message_type,
			lm.created_at     AS last_message_at,
			COUNT(unread.id)  AS unread_count
		FROM conversations c
		JOIN users owner   ON owner.id   = c.owner_id
		JOIN users artisan ON artisan.id = c.artisan_id

		LEFT JOIN LATERAL (
			SELECT content, msg_type, created_at
			FROM   messages
			WHERE  conversation_id = c.id
			ORDER  BY created_at DESC
			LIMIT  1
		) lm ON true

		LEFT JOIN messages unread
			ON  unread.conversation_id = c.id
			AND unread.sender_id      <> $1
			AND unread.is_read         = FALSE

		WHERE (c.owner_id = $1 OR c.artisan_id = $1)
		  AND c.deleted_at IS NULL
		GROUP BY c.id, owner.first_name, owner.last_name,
		         artisan.first_name, artisan.last_name,
		         lm.content, lm.msg_type, lm.created_at
		ORDER BY c.updated_at DESC
		LIMIT  $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("failed to fetch conversations: %v", err)
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
		ID          uuid.UUID       `json:"id"`
		Owner       ParticipantInfo `json:"owner"`
		Artisan     ParticipantInfo `json:"artisan"`
		LastMessage *LastMessage    `json:"last_message"`
		UnreadCount int             `json:"unread_count"`
		UpdatedAt   time.Time       `json:"updated_at"`
	}

	conversations := make([]ConversationItem, 0)
	for rows.Next() {
		var (
			item        ConversationItem
			ownerID     uuid.UUID
			ownerName   string
			artisanID   uuid.UUID
			artisanName string
			lastContent *string
			lastType    *string
			lastAt      *time.Time
			unread      int
		)

		if err := rows.Scan(
			&item.ID,
			&ownerID, &ownerName,
			&artisanID, &artisanName,
			&item.UpdatedAt,
			&lastContent, &lastType, &lastAt,
			&unread,
		); err != nil {
			utils.Logger.Errorf("failed to scan conversation: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		item.Owner = ParticipantInfo{ID: ownerID, Name: ownerName}
		item.Artisan = ParticipantInfo{ID: artisanID, Name: artisanName}
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
		utils.Logger.Errorf("conversation row error: %v", err)
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
// GET /chat/conversations/{id}/messages
// ============================================================================

// GetMessages godoc
// @Summary      Get message history
// @Description  Returns paginated message history for a conversation. The caller must be a participant. Supports limit and offset query params.
// @Tags         Chat
// @Produce      json
// @Param        id      path   string  true   "Conversation UUID"
// @Param        limit   query  int     false  "Max messages to return (default 50, max 100)"
// @Param        offset  query  int     false  "Number of messages to skip (default 0)"
// @Success      200  {object}  object{status=string,count=int,data=[]chat.Message,pagination=object{total=int,limit=int,offset=int,total_pages=int}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /chat/conversations/{id}/messages [get]
// @Security     BearerAuth
func GetMessages(w http.ResponseWriter, r *http.Request) {
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

	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("failed to fetch conversation: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != ownerID && userID != artisanID {
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
	err = db.QueryRow(ctx,
		`SELECT COUNT(*) FROM messages WHERE conversation_id = $1`,
		convoID,
	).Scan(&totalCount)
	if err != nil {
		utils.Logger.Errorf("failed to count messages: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(ctx, `
		SELECT id, conversation_id, sender_id, sender_role,
		       content, msg_type, is_read, created_at
		FROM   messages
		WHERE  conversation_id = $1
		ORDER  BY created_at ASC
		LIMIT  $2 OFFSET $3
	`, convoID, limit, offset)
	if err != nil {
		utils.Logger.Errorf("failed to fetch messages: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := make([]chat.Message, 0)
	for rows.Next() {
		var msg chat.Message
		if err := rows.Scan(
			&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.SenderRole,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		); err != nil {
			utils.Logger.Errorf("failed to scan message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		utils.Logger.Errorf("message row error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
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
// PATCH /chat/conversations/{id}/read
// ============================================================================

// MarkMessagesRead godoc
// @Summary      Mark messages as read
// @Description  Marks all unread messages in a conversation (sent by the other party) as read. Also notifies the sender via WebSocket that their messages were read.
// @Tags         Chat
// @Produce      json
// @Param        id  path  string  true  "Conversation UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /chat/conversations/{id}/read [patch]
// @Security     BearerAuth
func MarkMessagesRead(w http.ResponseWriter, r *http.Request) {
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

	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != ownerID && userID != artisanID {
		utils.WriteError(w, "you are not a participant of this conversation", http.StatusForbidden)
		return
	}

	_, err = db.Exec(ctx, `
		UPDATE messages
		SET    is_read = TRUE
		WHERE  conversation_id = $1
		  AND  sender_id <> $2
		  AND  is_read = FALSE
	`, convoID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to mark messages as read: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if Hub != nil {
		otherID := ownerID
		if userID == ownerID {
			otherID = artisanID
		}
		payload, _ := json.Marshal(chat.OutgoingWS{
			Type: "read",
			Payload: chat.ReadPayload{
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
// POST /chat/conversations/{id}/images
// ============================================================================

// SendImageMessage godoc
// @Summary      Send image message(s)
// @Description  Uploads 1–5 images to Cloudinary and inserts each as an image message in the conversation. Both participants receive a WebSocket push for each image. The recipient also gets a push notification.
// @Tags         Chat
// @Accept       mpfd
// @Produce      json
// @Param        id      path      string  true  "Conversation UUID"
// @Param        images  formData  file    true  "Image file(s) — max 5"
// @Success      200  {object}  object{status=string,message=string,messages=[]chat.Message}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /chat/conversations/{id}/images [post]
// @Security     BearerAuth
func SendImageMessage(w http.ResponseWriter, r *http.Request) {
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

	// Confirm caller is a participant
	var ownerID, artisanID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT owner_id, artisan_id FROM conversations WHERE id = $1 AND deleted_at IS NULL`,
		convoID,
	).Scan(&ownerID, &artisanID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "conversation not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != ownerID && userID != artisanID {
		utils.WriteError(w, "you are not a participant of this conversation", http.StatusForbidden)
		return
	}

	var chatExpiresAt *time.Time
	_ = db.QueryRow(ctx,
		`SELECT chat_expires_at FROM conversations WHERE id = $1`,
		convoID,
	).Scan(&chatExpiresAt)
	if chatExpiresAt != nil && time.Now().After(*chatExpiresAt) {
		utils.WriteError(w, "this conversation has been closed 24 hours after booking completion", http.StatusForbidden)
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
			utils.Logger.Warnf("failed to open image %s: %v", h.Filename, err)
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
		utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
		return
	}

	urls, _, err := handlers.UploadFilesConcurrently(ctx, cloud, files, "chat/images")
	if err != nil || len(urls) == 0 {
		utils.Logger.Errorf("failed to upload chat images: %v", err)
		utils.WriteError(w, "failed to upload images", http.StatusInternalServerError)
		return
	}

	recipientID := ownerID
	if userID == ownerID {
		recipientID = artisanID
	}

	var savedMessages []chat.Message
	for _, url := range urls {
		var msg chat.Message
		err = db.QueryRow(ctx, `
			INSERT INTO messages (conversation_id, sender_id, sender_role, content, msg_type)
			VALUES ($1, $2, $3, $4, 'image')
			RETURNING id, conversation_id, sender_id, sender_role, content, msg_type, is_read, created_at
		`, convoID, userID, role, url).Scan(
			&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.SenderRole,
			&msg.Content, &msg.MsgType, &msg.IsRead, &msg.CreatedAt,
		)
		if err != nil {
			utils.Logger.Errorf("failed to insert image message: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if Hub != nil {
			payload, _ := json.Marshal(chat.OutgoingWS{Type: "message", Payload: msg})
			Hub.DeliverTo(userID, payload)
			Hub.DeliverTo(recipientID, payload)
		}

		savedMessages = append(savedMessages, msg)
	}

	if chathub.PushNotifier != nil {
		go chathub.PushNotifier(recipientID, "New Image", fmt.Sprintf("%d image(s) sent", len(savedMessages)))
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":   "success",
		"message":  fmt.Sprintf("%d image(s) sent", len(savedMessages)),
		"messages": savedMessages,
	})
}

// ============================================================================
// DELETE /chat/conversations/{id}
// ============================================================================

// DeleteConversation godoc
// @Summary      Delete a conversation
// @Description  Soft-deletes a conversation. Only the client (owner) who created it can delete it. The conversation and its messages remain in the database for audit purposes.
// @Tags         Chat
// @Produce      json
// @Param        id  path  string  true  "Conversation UUID"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /chat/conversations/{id} [delete]
// @Security     BearerAuth
func DeleteConversation(w http.ResponseWriter, r *http.Request) {
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
		UPDATE conversations
		SET    deleted_at = NOW()
		WHERE  id = $1
		  AND  owner_id = $2
		  AND  deleted_at IS NULL
	`, convoID, userID)
	if err != nil {
		utils.Logger.Errorf("failed to delete conversation: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "conversation not found or you do not have permission to delete it", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "conversation deleted",
	})
}
