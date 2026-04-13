package calls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	chatHandler "leti_server/internal/api/handlers/chat"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	getstream "github.com/GetStream/getstream-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var streamClient *getstream.Stream

func getStreamClient() (*getstream.Stream, error) {
	if streamClient != nil {
		return streamClient, nil
	}
	apiKey := os.Getenv("STREAM_API_KEY")
	apiSecret := os.Getenv("STREAM_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("STREAM_API_KEY or STREAM_API_SECRET not configured")
	}
	c, err := getstream.NewClient(apiKey, apiSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to init stream client: %w", err)
	}
	streamClient = c
	return streamClient, nil
}

type participantInfo struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	Role      string    `json:"role"`
}

func fetchParticipantInfo(ctx context.Context, userID uuid.UUID) (participantInfo, error) {
	var info participantInfo
	info.ID = userID

	var avatarJSON []byte
	err := sqlconnect.DB.QueryRow(ctx,
		`SELECT username, avatar, active_role
		 FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&info.Username, &avatarJSON, &info.Role)
	if err != nil {
		return info, err
	}

	if len(avatarJSON) > 0 {
		type av struct {
			URL string `json:"url"`
		}
		var a av
		if json.Unmarshal(avatarJSON, &a) == nil && a.URL != "" {
			info.AvatarURL = &a.URL
		}
	}
	return info, nil
}

func ensureStreamUser(ctx context.Context, sc *getstream.Stream, info participantInfo) {
	image := ""
	if info.AvatarURL != nil {
		image = *info.AvatarURL
	}
	userIDStr := info.ID.String()
	_, err := sc.UpdateUsers(ctx, &getstream.UpdateUsersRequest{
		Users: map[string]getstream.UserRequest{
			userIDStr: {
				ID:    userIDStr,
				Name:  getstream.PtrTo(info.Username),
				Image: getstream.PtrTo(image),
				Role:  getstream.PtrTo("user"),
			},
		},
	})
	if err != nil {
		utils.Logger.Warnf("stream UpdateUsers for %s: %v", userIDStr, err)
	}
}

// ============================================================================
// Context types
//
//	"booking" — artisan booking  (client ↔ artisan)
//	"order"   — shortlet order   (client ↔ owner)
// ============================================================================

func buildProviderCallID(contextType string, contextID, callID uuid.UUID) string {
	prefix := string(contextType[0]) // "b" or "o"
	cID := strings.ReplaceAll(contextID.String(), "-", "")[:16]
	kID := strings.ReplaceAll(callID.String(), "-", "")
	return fmt.Sprintf("%s_%s_%s", prefix, cID, kID)
}

// validateCallContext checks that:
//  1. The context (booking/order) exists and is in an active state.
//  2. The caller is a legitimate participant.
//  3. The callee is the correct counterpart.
//
// No phone numbers or emails are returned — privacy is preserved by design.
func validateCallContext(
	ctx context.Context,
	contextType string,
	contextID, callerID, calleeID uuid.UUID,
) error {
	db := sqlconnect.DB

	switch contextType {

	case "booking":
		var clientID, artisanID uuid.UUID
		var status, paymentStatus string

		err := db.QueryRow(ctx, `
			SELECT client_id, artisan_id, status, payment_status
			FROM artisan_bookings
			WHERE id = $1
		`, contextID).Scan(&clientID, &artisanID, &status, &paymentStatus)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("booking not found")
			}
			return fmt.Errorf("internal server error")
		}

		if paymentStatus != "paid" {
			return fmt.Errorf("calls are only available for paid bookings")
		}

		activeStatuses := map[string]bool{
			"confirmed": true, "completed": true,
			"awaiting_client_confirmation": true,
		}
		if !activeStatuses[status] {
			return fmt.Errorf("calls are only available for confirmed or active bookings")
		}

		isClient := callerID == clientID
		isArtisan := callerID == artisanID
		if !isClient && !isArtisan {
			return fmt.Errorf("you are not a participant of this booking")
		}

		if isClient && calleeID != artisanID {
			return fmt.Errorf("callee is not the artisan on this booking")
		}
		if isArtisan && calleeID != clientID {
			return fmt.Errorf("callee is not the client on this booking")
		}

	case "order":
		var clientID, ownerID uuid.UUID
		var status, paymentStatus string

		err := db.QueryRow(ctx, `
			SELECT client_id, owner_id, status, payment_status
			FROM orders
			WHERE id = $1
		`, contextID).Scan(&clientID, &ownerID, &status, &paymentStatus)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("order not found")
			}
			return fmt.Errorf("internal server error")
		}

		if paymentStatus != "paid" {
			return fmt.Errorf("calls are only available for paid orders")
		}

		activeStatuses := map[string]bool{
			"confirmed": true, "checked_in": true, "completed": true,
		}
		if !activeStatuses[status] {
			return fmt.Errorf("calls are only available for confirmed or active orders")
		}

		isClient := callerID == clientID
		isOwner := callerID == ownerID
		if !isClient && !isOwner {
			return fmt.Errorf("you are not a participant of this order")
		}

		if isClient && calleeID != ownerID {
			return fmt.Errorf("callee is not the owner of this order")
		}
		if isOwner && calleeID != clientID {
			return fmt.Errorf("callee is not the client on this order")
		}

	default:
		return fmt.Errorf("context_type must be 'booking' or 'order'")
	}

	return nil
}

// ============================================================================
// POST /calls/start
// ============================================================================

// StartCall godoc
// @Summary      Start a call
// @Description  Initiates an audio-only call between two participants of a booking or order.
// @Description  context_type must be "booking" (client↔artisan) or "order" (client↔owner).
// @Description  No phone numbers are ever exposed — Stream handles the media layer.
// @Description  A push notification and WebSocket event are sent to the callee.
// @Tags         Calls
// @Accept       json
// @Produce      json
// @Param        body  body  object{context_type=string,context_id=string,callee_id=string,client_call_id=string}  true  "Call request"
// @Success      200   {object}  object{status=string,message=string,data=object}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Failure      503   {object}  object{error=string}
// @Router       /calls/start [post]
// @Security     BearerAuth
func StartCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	callerID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		ContextType  string  `json:"context_type"`
		ContextID    string  `json:"context_id"`
		CalleeID     string  `json:"callee_id"`
		ClientCallID *string `json:"client_call_id"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContextType != "booking" && req.ContextType != "order" {
		utils.WriteError(w, "context_type must be 'booking' or 'order'", http.StatusBadRequest)
		return
	}

	contextID, err := uuid.Parse(req.ContextID)
	if err != nil {
		utils.WriteError(w, "invalid context_id", http.StatusBadRequest)
		return
	}

	calleeID, err := uuid.Parse(req.CalleeID)
	if err != nil {
		utils.WriteError(w, "invalid callee_id", http.StatusBadRequest)
		return
	}

	if callerID == calleeID {
		utils.WriteError(w, "you cannot call yourself", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if req.ClientCallID != nil && *req.ClientCallID != "" {
		var exists bool
		db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM calls WHERE client_call_id = $1)`,
			*req.ClientCallID,
		).Scan(&exists)
		if exists {
			utils.WriteError(w, "duplicate call request", http.StatusConflict)
			return
		}
	}

	if err := validateCallContext(ctx, req.ContextType, contextID, callerID, calleeID); err != nil {
		status := http.StatusForbidden
		if err.Error() == "booking not found" || err.Error() == "order not found" {
			status = http.StatusNotFound
		}
		utils.WriteError(w, err.Error(), status)
		return
	}

	var alreadyActive bool
	db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM calls
			WHERE context_type = $1 AND context_id = $2
			  AND state IN ('ringing','accepted')
		)
	`, req.ContextType, contextID).Scan(&alreadyActive)
	if alreadyActive {
		utils.WriteError(w, "a call is already active for this context", http.StatusConflict)
		return
	}

	callerInfo, err := fetchParticipantInfo(ctx, callerID)
	if err != nil {
		utils.Logger.Errorf("fetch caller info: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	calleeInfo, err := fetchParticipantInfo(ctx, calleeID)
	if err != nil {
		utils.Logger.Errorf("fetch callee info: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	sc, err := getStreamClient()
	if err != nil {
		utils.Logger.Errorf("stream client: %v", err)
		utils.WriteError(w, "calling service unavailable", http.StatusServiceUnavailable)
		return
	}

	ensureStreamUser(ctx, sc, callerInfo)
	ensureStreamUser(ctx, sc, calleeInfo)

	callID := uuid.New()
	providerCallID := buildProviderCallID(req.ContextType, contextID, callID)

	callerIDStr := callerID.String()
	calleeIDStr := calleeID.String()

	streamCall := sc.Video().Call("audio_room", providerCallID)
	_, err = streamCall.GetOrCreate(ctx, &getstream.GetOrCreateCallRequest{
		Data: &getstream.CallRequest{
			CreatedByID: &callerIDStr,
			Members: []getstream.MemberRequest{
				{UserID: callerIDStr},
				{UserID: calleeIDStr},
			},
		},
	})
	if err != nil {
		utils.Logger.Errorf("Stream GetOrCreate: %v", err)
		utils.WriteError(w, "failed to create call room", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, `
		INSERT INTO calls (
			id, provider, provider_call_type, provider_call_id,
			context_type, context_id,
			caller_id, callee_id,
			audio_only, state, client_call_id
		) VALUES ($1, 'stream', 'audio_room', $2, $3, $4, $5, $6, TRUE, 'ringing', $7)
	`, callID, providerCallID, req.ContextType, contextID,
		callerID, calleeID, req.ClientCallID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			utils.WriteError(w, "duplicate call request", http.StatusConflict)
			return
		}
		utils.Logger.Errorf("persist call: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()

	wsPayload := handlers.BuildWSPayload("call.incoming", map[string]interface{}{
		"call_id":      callID.String(),
		"state":        "ringing",
		"context_type": req.ContextType,
		"context_id":   contextID.String(),
		"from_user": map[string]interface{}{
			"id":         callerInfo.ID.String(),
			"username":   callerInfo.Username,
			"avatar_url": callerInfo.AvatarURL,
			"role":       callerInfo.Role,
		},
		"to_user": map[string]interface{}{
			"id":         calleeInfo.ID.String(),
			"username":   calleeInfo.Username,
			"avatar_url": calleeInfo.AvatarURL,
			"role":       calleeInfo.Role,
		},
		"timestamp": now.Format(time.RFC3339),
	})

	if chatHandler.Hub != nil {
		chatHandler.Hub.DeliverTo(calleeID, wsPayload)
	}

	go handlers.SendPushToUser(calleeID,
		fmt.Sprintf("📞 %s is calling", callerInfo.Username),
		"Tap to answer",
		map[string]string{
			"type":         "call.incoming",
			"call_id":      callID.String(),
			"caller_name":  callerInfo.Username,
			"context_type": req.ContextType,
			"context_id":   contextID.String(),
		},
	)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "call started",
		"data": map[string]interface{}{
			"call_id":            callID,
			"provider":           "stream",
			"provider_call_type": "audio_room",
			"provider_call_id":   providerCallID,
			"audio_only":         true,
			"state":              "ringing",
			"context":            map[string]interface{}{"type": req.ContextType, "id": contextID},
			"participants":       map[string]interface{}{"caller": callerInfo, "callee": calleeInfo},
			"created_at":         now,
			"expires_at":         now.Add(30 * time.Second),
		},
	})
}

// ============================================================================
// POST /calls/token
// ============================================================================

// GetCallToken godoc
// @Summary      Get a Stream token for a call
// @Description  Issues a time-limited Stream token for the authenticated participant of a call. The caller must be either the caller or callee on the call.
// @Tags         Calls
// @Accept       json
// @Produce      json
// @Param        body  body  object{call_id=string}  true  "Call UUID"
// @Success      200   {object}  object{status=string,data=object{provider=string,api_key=string,token=string,expires_at=string,user=object}}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Failure      503   {object}  object{error=string}
// @Router       /calls/token [post]
// @Security     BearerAuth
func GetCallToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		CallID string `json:"call_id"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	callUUID, err := uuid.Parse(req.CallID)
	if err != nil {
		utils.WriteError(w, "invalid call_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var callerID, calleeID uuid.UUID
	var callState string
	err = sqlconnect.DB.QueryRow(ctx,
		`SELECT caller_id, callee_id, state FROM calls WHERE id = $1`, callUUID,
	).Scan(&callerID, &calleeID, &callState)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "call not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != callerID && userID != calleeID {
		utils.WriteError(w, "you are not a participant of this call", http.StatusForbidden)
		return
	}

	terminalStates := map[string]bool{
		"ended": true, "rejected": true, "missed": true, "failed": true,
	}
	if terminalStates[callState] {
		utils.WriteError(w, "call is no longer active", http.StatusConflict)
		return
	}

	sc, err := getStreamClient()
	if err != nil {
		utils.WriteError(w, "calling service unavailable", http.StatusServiceUnavailable)
		return
	}

	token, err := sc.CreateToken(userID.String(), getstream.WithExpiration(time.Hour))
	if err != nil {
		utils.Logger.Errorf("CreateToken for %s: %v", userID, err)
		utils.WriteError(w, "failed to generate call token", http.StatusInternalServerError)
		return
	}

	info, err := fetchParticipantInfo(ctx, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"provider":   "stream",
			"api_key":    os.Getenv("STREAM_API_KEY"),
			"token":      token,
			"expires_at": time.Now().UTC().Add(time.Hour),
			"user":       info,
		},
	})
}

// ============================================================================
// POST /calls/{call_id}/accept
// ============================================================================

// AcceptCall godoc
// @Summary      Accept an incoming call
// @Description  The callee accepts a ringing call. Both participants are notified via WebSocket.
// @Tags         Calls
// @Produce      json
// @Param        call_id  path  string  true  "Call UUID"
// @Success      200      {object}  object{status=string,message=string,data=object}
// @Failure      403      {object}  object{error=string}
// @Failure      404      {object}  object{error=string}
// @Failure      409      {object}  object{error=string}
// @Router       /calls/{call_id}/accept [post]
// @Security     BearerAuth
func AcceptCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	callUUID, err := uuid.Parse(r.PathValue("call_id"))
	if err != nil {
		utils.WriteError(w, "invalid call_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var callerID, calleeID uuid.UUID
	var state string
	err = sqlconnect.DB.QueryRow(ctx,
		`SELECT caller_id, callee_id, state FROM calls WHERE id = $1`, callUUID,
	).Scan(&callerID, &calleeID, &state)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "call not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != calleeID {
		utils.WriteError(w, "only the callee can accept a call", http.StatusForbidden)
		return
	}
	if state != "ringing" {
		utils.WriteError(w, fmt.Sprintf("cannot accept a call in '%s' state", state), http.StatusConflict)
		return
	}

	now := time.Now().UTC()
	sqlconnect.DB.Exec(ctx,
		`UPDATE calls SET state = 'accepted', started_at = $1 WHERE id = $2`, now, callUUID)

	payload := handlers.BuildWSPayload("call.accepted", map[string]interface{}{
		"call_id":    callUUID.String(),
		"state":      "accepted",
		"started_at": now,
	})
	if chatHandler.Hub != nil {
		chatHandler.Hub.DeliverTo(callerID, payload)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "call accepted",
		"data": map[string]interface{}{
			"call_id":    callUUID,
			"state":      "accepted",
			"started_at": now,
		},
	})
}

// ============================================================================
// POST /calls/{call_id}/reject
// ============================================================================

// RejectCall godoc
// @Summary      Reject an incoming call
// @Description  The callee rejects a ringing call. The caller is notified via WebSocket.
// @Tags         Calls
// @Accept       json
// @Produce      json
// @Param        call_id  path  string  true  "Call UUID"
// @Param        body     body  object{reason=string}  false  "Optional reject reason"
// @Success      200      {object}  object{status=string,message=string,data=object}
// @Failure      403      {object}  object{error=string}
// @Failure      404      {object}  object{error=string}
// @Failure      409      {object}  object{error=string}
// @Router       /calls/{call_id}/reject [post]
// @Security     BearerAuth
func RejectCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	callUUID, err := uuid.Parse(r.PathValue("call_id"))
	if err != nil {
		utils.WriteError(w, "invalid call_id", http.StatusBadRequest)
		return
	}

	type request struct {
		Reason string `json:"reason"`
	}
	var req request
	json.NewDecoder(r.Body).Decode(&req)
	defer r.Body.Close()
	if req.Reason == "" {
		req.Reason = "declined"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var callerID, calleeID uuid.UUID
	var state string
	err = sqlconnect.DB.QueryRow(ctx,
		`SELECT caller_id, callee_id, state FROM calls WHERE id = $1`, callUUID,
	).Scan(&callerID, &calleeID, &state)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "call not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != calleeID {
		utils.WriteError(w, "only the callee can reject a call", http.StatusForbidden)
		return
	}
	if state != "ringing" {
		utils.WriteError(w, fmt.Sprintf("cannot reject a call in '%s' state", state), http.StatusConflict)
		return
	}

	now := time.Now().UTC()
	sqlconnect.DB.Exec(ctx,
		`UPDATE calls SET state = 'rejected', ended_at = $1, end_reason = $2 WHERE id = $3`,
		now, req.Reason, callUUID)

	payload := handlers.BuildWSPayload("call.rejected", map[string]interface{}{
		"call_id":    callUUID.String(),
		"state":      "rejected",
		"ended_at":   now,
		"end_reason": req.Reason,
	})
	if chatHandler.Hub != nil {
		chatHandler.Hub.DeliverTo(callerID, payload)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "call rejected",
		"data": map[string]interface{}{
			"call_id":    callUUID,
			"state":      "rejected",
			"ended_at":   now,
			"end_reason": req.Reason,
		},
	})
}

// ============================================================================
// POST /calls/end
// ============================================================================

// EndCall godoc
// @Summary      End an active call
// @Description  Either participant can end a call that is in ringing or accepted state. The other participant is notified via WebSocket and the Stream room is closed.
// @Tags         Calls
// @Accept       json
// @Produce      json
// @Param        body  body  object{call_id=string,reason=string}  true  "End request. reason: hangup | declined | missed | timeout | network_error | failed"
// @Success      200   {object}  object{status=string,message=string,data=object}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /calls/end [post]
// @Security     BearerAuth
func EndCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type request struct {
		CallID string `json:"call_id"`
		Reason string `json:"reason"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	validReasons := map[string]bool{
		"hangup": true, "declined": true, "missed": true,
		"timeout": true, "network_error": true, "failed": true,
	}
	if req.Reason == "" {
		req.Reason = "hangup"
	}
	if !validReasons[req.Reason] {
		utils.WriteError(w, "invalid reason", http.StatusBadRequest)
		return
	}

	callUUID, err := uuid.Parse(req.CallID)
	if err != nil {
		utils.WriteError(w, "invalid call_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var callerID, calleeID uuid.UUID
	var state string
	var startedAt *time.Time
	var providerCallID string

	err = sqlconnect.DB.QueryRow(ctx, `
		SELECT caller_id, callee_id, state, started_at, provider_call_id
		FROM calls WHERE id = $1
	`, callUUID).Scan(&callerID, &calleeID, &state, &startedAt, &providerCallID)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "call not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != callerID && userID != calleeID {
		utils.WriteError(w, "you are not a participant of this call", http.StatusForbidden)
		return
	}

	terminalStates := map[string]bool{
		"ended": true, "rejected": true, "missed": true, "failed": true,
	}
	if terminalStates[state] {
		utils.WriteError(w, "call is already ended", http.StatusConflict)
		return
	}

	finalState := "ended"
	switch req.Reason {
	case "missed":
		finalState = "missed"
	case "failed", "network_error":
		finalState = "failed"
	}

	now := time.Now().UTC()
	sqlconnect.DB.Exec(ctx,
		`UPDATE calls SET state = $1, ended_at = $2, end_reason = $3 WHERE id = $4`,
		finalState, now, req.Reason, callUUID)

	durationSeconds := 0
	if startedAt != nil {
		durationSeconds = int(now.Sub(*startedAt).Seconds())
	}

	otherID := calleeID
	if userID == calleeID {
		otherID = callerID
	}

	payload := handlers.BuildWSPayload("call.ended", map[string]interface{}{
		"call_id":          callUUID.String(),
		"state":            finalState,
		"ended_at":         now,
		"end_reason":       req.Reason,
		"duration_seconds": durationSeconds,
	})
	if chatHandler.Hub != nil {
		chatHandler.Hub.DeliverTo(otherID, payload)
	}

	// Close the Stream room asynchronously
	go func(pCallID string) {
		sc, err := getStreamClient()
		if err != nil {
			return
		}
		sc.Video().Call("audio_room", pCallID).End(context.Background(), &getstream.EndCallRequest{})
	}(providerCallID)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "call ended",
		"data": map[string]interface{}{
			"call_id":          callUUID,
			"state":            finalState,
			"ended_at":         now,
			"end_reason":       req.Reason,
			"duration_seconds": durationSeconds,
		},
	})
}

// ============================================================================
// GET /calls/{call_id}
// ============================================================================

// GetCall godoc
// @Summary      Get call details
// @Description  Returns full details of a call. The caller must be a participant.
// @Tags         Calls
// @Produce      json
// @Param        call_id  path  string  true  "Call UUID"
// @Success      200      {object}  object{status=string,data=object}
// @Failure      403      {object}  object{error=string}
// @Failure      404      {object}  object{error=string}
// @Router       /calls/{call_id} [get]
// @Security     BearerAuth
func GetCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	callUUID, err := uuid.Parse(r.PathValue("call_id"))
	if err != nil {
		utils.WriteError(w, "invalid call_id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		id, callerID, calleeID   uuid.UUID
		provider, pCallType, pID string
		contextType              string
		contextID                uuid.UUID
		audioOnly                bool
		state                    string
		endReason                *string
		startedAt, endedAt       *time.Time
		durationSeconds          int
		createdAt                time.Time
	)

	err = sqlconnect.DB.QueryRow(ctx, `
		SELECT id, provider, provider_call_type, provider_call_id,
		       context_type, context_id,
		       caller_id, callee_id, audio_only,
		       state, end_reason,
		       started_at, ended_at, duration_seconds, created_at
		FROM calls WHERE id = $1
	`, callUUID).Scan(
		&id, &provider, &pCallType, &pID,
		&contextType, &contextID,
		&callerID, &calleeID, &audioOnly,
		&state, &endReason,
		&startedAt, &endedAt, &durationSeconds, &createdAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "call not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if userID != callerID && userID != calleeID {
		utils.WriteError(w, "you are not a participant of this call", http.StatusForbidden)
		return
	}

	callerInfo, _ := fetchParticipantInfo(ctx, callerID)
	calleeInfo, _ := fetchParticipantInfo(ctx, calleeID)

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"call_id":            id,
			"provider":           provider,
			"provider_call_type": pCallType,
			"provider_call_id":   pID,
			"audio_only":         audioOnly,
			"state":              state,
			"context":            map[string]interface{}{"type": contextType, "id": contextID},
			"participants":       map[string]interface{}{"caller": callerInfo, "callee": calleeInfo},
			"created_at":         createdAt,
			"started_at":         startedAt,
			"ended_at":           endedAt,
			"end_reason":         endReason,
			"duration_seconds":   durationSeconds,
		},
	})
}

// ============================================================================
// GET /calls/history
// ============================================================================

// GetCallHistory godoc
// @Summary      Get call history
// @Description  Returns a paginated list of calls for the authenticated user. Optionally filter by context_type (booking|order) and context_id.
// @Tags         Calls
// @Produce      json
// @Param        context_type  query  string  false  "Filter: booking | order"
// @Param        context_id    query  string  false  "Filter by specific booking or order UUID"
// @Param        page          query  int     false  "Page (default 1)"
// @Param        limit         query  int     false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,data=array,meta=object}
// @Failure      401  {object}  object{error=string}
// @Router       /calls/history [get]
// @Security     BearerAuth
func GetCallHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	contextType := r.URL.Query().Get("context_type")
	contextIDStr := r.URL.Query().Get("context_id")
	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	args := []interface{}{userID, userID}
	where := "(caller_id = $1 OR callee_id = $2)"
	argIdx := 3

	if contextType != "" {
		where += fmt.Sprintf(" AND context_type = $%d", argIdx)
		args = append(args, contextType)
		argIdx++
	}
	if contextIDStr != "" {
		if cid, err := uuid.Parse(contextIDStr); err == nil {
			where += fmt.Sprintf(" AND context_id = $%d", argIdx)
			args = append(args, cid)
			argIdx++
		}
	}

	var total int
	sqlconnect.DB.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM calls WHERE %s`, where), args...,
	).Scan(&total)

	args = append(args, limit, offset)
	rows, err := sqlconnect.DB.Query(ctx, fmt.Sprintf(`
		SELECT id, state, context_type, context_id,
		       caller_id, callee_id,
		       started_at, ended_at, duration_seconds, end_reason
		FROM calls WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1), args...)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type historyItem struct {
		CallID          uuid.UUID       `json:"call_id"`
		State           string          `json:"state"`
		ContextType     string          `json:"context_type"`
		ContextID       uuid.UUID       `json:"context_id"`
		OtherParty      participantInfo `json:"other_party"`
		StartedAt       *time.Time      `json:"started_at"`
		EndedAt         *time.Time      `json:"ended_at"`
		DurationSeconds int             `json:"duration_seconds"`
		EndReason       *string         `json:"end_reason"`
	}

	items := make([]historyItem, 0)
	for rows.Next() {
		var item historyItem
		var cID, clID uuid.UUID
		if err := rows.Scan(
			&item.CallID, &item.State,
			&item.ContextType, &item.ContextID,
			&cID, &clID,
			&item.StartedAt, &item.EndedAt,
			&item.DurationSeconds, &item.EndReason,
		); err != nil {
			continue
		}

		otherID := clID
		if userID == clID {
			otherID = cID
		}
		info, _ := fetchParticipantInfo(ctx, otherID)
		item.OtherParty = info
		items = append(items, item)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data":   items,
		"meta":   map[string]int{"page": page, "limit": limit, "total": total},
	})
}

// ============================================================================
// Background worker — auto-expire ringing calls after 30s
// Call from main.go:  go calls.StartRingingTimeoutWorker(ctx)
// ============================================================================
func StartRingingTimeoutWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			utils.Logger.Info("ringing timeout worker stopped")
			return
		case <-ticker.C:
			func() {
				tCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				rows, err := sqlconnect.DB.Query(tCtx, `
					UPDATE calls
					SET state = 'missed', ended_at = NOW(), end_reason = 'timeout'
					WHERE state = 'ringing'
					  AND created_at < NOW() - INTERVAL '30 seconds'
					RETURNING id, caller_id, callee_id
				`)
				if err != nil {
					utils.Logger.Warnf("ringing timeout query: %v", err)
					return
				}
				defer rows.Close()

				for rows.Next() {
					var callID, callerID, calleeID uuid.UUID
					if rows.Scan(&callID, &callerID, &calleeID) != nil {
						continue
					}
					utils.Logger.Infof("call %s timed out (missed)", callID)

					if chatHandler.Hub != nil {
						p := handlers.BuildWSPayload("call.missed", map[string]interface{}{
							"call_id":    callID.String(),
							"state":      "missed",
							"end_reason": "timeout",
							"ended_at":   time.Now().UTC(),
						})
						chatHandler.Hub.DeliverTo(callerID, p)
						chatHandler.Hub.DeliverTo(calleeID, p)
					}
				}
			}()
		}
	}
}
