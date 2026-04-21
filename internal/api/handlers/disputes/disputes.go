package disputes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"leti_server/internal/api/handlers"
	"leti_server/internal/dto"
	disputemodels "leti_server/internal/models/support"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DisputeEvidenceSwagger mirrors support.DisputeEvidence for Swagger.
type DisputeEvidenceSwagger struct {
	URL      string `json:"url"       example:"https://res.cloudinary.com/demo/image/upload/sample.jpg"`
	PublicID string `json:"public_id" example:"disputes/sample"`
}

type FileDisputeRequest struct {
	Reason   string                   `json:"reason"             example:"Artisan did not complete the agreed work"`
	Evidence []DisputeEvidenceSwagger `json:"evidence,omitempty"`
}

// ============================================================================
// POST /dispute-centre/jobs/{id}/dispute  — client OR artisan files a job dispute
// ============================================================================

// FileJobDispute godoc
// @Summary      File a job dispute
// @Description  Allows an authenticated user to file a dispute against a job they are party to. Evidence images can be included.
// @Tags         Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                  true  "Job UUID"
// @Param        body  body      FileDisputeRequest      true  "Dispute details"
// @Success      201   {object}  object{status=string,message=string,data=object{id=string,job_id=string,filed_by=string,reason=string,status=string,created_at=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}  "Dispute already exists for this job"
// @Router       /dispute-centre/jobs/{id}/dispute [post]
// @Security     BearerAuth
func FileJobDispute(w http.ResponseWriter, r *http.Request) {
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

	if role != "client" && role != "artisan" {
		utils.WriteError(w, "only clients and artisans can file job disputes", http.StatusForbidden)
		return
	}

	jobID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid job id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := r.ParseMultipartForm(30 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	reason := r.FormValue("reason")
	if reason == "" {
		utils.WriteError(w, "reason is required", http.StatusBadRequest)
		return
	}

	var clientID uuid.UUID
	var assignedArtisanID *uuid.UUID
	var jobStatus string
	err = db.QueryRow(ctx, `
		SELECT client_id, assigned_artisan_id, status
		FROM jobs WHERE id = $1
	`, jobID).Scan(&clientID, &assignedArtisanID, &jobStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "job not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var respondentID uuid.UUID
	switch role {
	case "client":
		if clientID != userID {
			utils.WriteError(w, "you do not own this job", http.StatusForbidden)
			return
		}
		if assignedArtisanID == nil {
			utils.WriteError(w, "no artisan assigned to this job", http.StatusBadRequest)
			return
		}
		respondentID = *assignedArtisanID
	case "artisan":
		if assignedArtisanID == nil || *assignedArtisanID != userID {
			utils.WriteError(w, "you are not assigned to this job", http.StatusForbidden)
			return
		}
		respondentID = clientID
	}

	allowedStatuses := map[string]bool{"completed": true, "in_progress": true}
	if !allowedStatuses[jobStatus] {
		utils.WriteError(w, "disputes can only be filed for in-progress or completed jobs", http.StatusBadRequest)
		return
	}

	var existingCount int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM job_disputes
		WHERE job_id = $1 AND status IN ('open','investigating')
	`, jobID).Scan(&existingCount)
	if existingCount > 0 {
		utils.WriteError(w, "an active dispute already exists for this job", http.StatusConflict)
		return
	}

	var evidence []disputemodels.DisputeEvidence

	if r.MultipartForm != nil && len(r.MultipartForm.File["evidence"]) > 0 {
		imageHeaders := r.MultipartForm.File["evidence"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 evidence images allowed", http.StatusBadRequest)
			return
		}

		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
			return
		}

		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()

		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, files, "disputes/evidence")
		if err != nil {
			utils.WriteError(w, "failed to upload evidence images", http.StatusInternalServerError)
			return
		}
		for i, url := range urls {
			evidence = append(evidence, disputemodels.DisputeEvidence{
				URL:      url,
				PublicID: publicIDs[i],
			})
		}
	}

	evidenceJSON, _ := json.Marshal(evidence)

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var disputeID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO job_disputes (job_id, filed_by, respondent_id, reason, evidence, status)
		VALUES ($1, $2, $3, $4, $5, 'open')
		RETURNING id
	`, jobID, userID, respondentID, reason, evidenceJSON).Scan(&disputeID)
	if err != nil {
		utils.Logger.Errorf("failed to create job dispute: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `
		UPDATE jobs SET status = 'disputed', updated_at = NOW() WHERE id = $1
	`, jobID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(
		context.Background(), respondentID, utils.NotifDisputeFiled,
		"Dispute Filed",
		fmt.Sprintf("A dispute has been filed against you for a job. Reason: %s", reason),
		map[string]interface{}{"job_id": jobID, "dispute_id": disputeID},
	)
	dto.PushFallback(respondentID,
		"Dispute Filed",
		"A dispute has been filed against you. Payment is on hold pending review.")

	utils.WriteJSON(w, map[string]interface{}{
		"status":     "success",
		"message":    "dispute filed successfully. Payment is frozen pending resolution.",
		"dispute_id": disputeID,
	})
}

// ============================================================================
// POST /dispute-centre/bookings/{id}/dispute  — client OR artisan files a booking dispute
// ============================================================================

// FileBookingDispute godoc
// @Summary      File a booking dispute
// @Description  Allows a client or artisan to file a dispute against the other party for a booking. The booking must be completed or awaiting client confirmation and must not already have an active dispute. Accepts multipart form data with an optional evidence field (max 5 images). Filing a dispute freezes the escrow payment pending admin resolution.
// @Tags         Disputes
// @Accept       multipart/form-data
// @Produce      json
// @Param        id        path      string  true   "Booking UUID"
// @Param        reason    formData  string  true   "Reason for the dispute"
// @Param        evidence  formData  file    false  "Evidence images (max 5)"
// @Success      200  {object}  object{status=string,message=string,dispute_id=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /dispute-centre/bookings/{id}/dispute [post]
// @Security     BearerAuth
func FileBookingDispute(w http.ResponseWriter, r *http.Request) {
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

	if role != "client" && role != "artisan" {
		utils.WriteError(w, "only clients and artisans can file booking disputes", http.StatusForbidden)
		return
	}

	bookingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid booking id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := r.ParseMultipartForm(30 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	reason := r.FormValue("reason")
	if reason == "" {
		utils.WriteError(w, "reason is required", http.StatusBadRequest)
		return
	}

	var clientID, artisanID uuid.UUID
	var bookingStatus string
	err = db.QueryRow(ctx, `
		SELECT client_id, artisan_id, status
		FROM artisan_bookings WHERE id = $1
	`, bookingID).Scan(&clientID, &artisanID, &bookingStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "booking not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var respondentID uuid.UUID
	switch role {
	case "client":
		if clientID != userID {
			utils.WriteError(w, "you did not make this booking", http.StatusForbidden)
			return
		}
		respondentID = artisanID
	case "artisan":
		if artisanID != userID {
			utils.WriteError(w, "you are not the artisan on this booking", http.StatusForbidden)
			return
		}
		respondentID = clientID
	}

	allowedStatuses := map[string]bool{
		"completed":                    true,
		"awaiting_client_confirmation": true,
	}
	if !allowedStatuses[bookingStatus] {
		utils.WriteError(w, "disputes can only be filed for completed or awaiting-confirmation bookings", http.StatusBadRequest)
		return
	}

	var existingCount int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM booking_disputes
		WHERE booking_id = $1 AND status IN ('open','investigating')
	`, bookingID).Scan(&existingCount)
	if existingCount > 0 {
		utils.WriteError(w, "an active dispute already exists for this booking", http.StatusConflict)
		return
	}

	var evidence []disputemodels.DisputeEvidence

	if r.MultipartForm != nil && len(r.MultipartForm.File["evidence"]) > 0 {
		imageHeaders := r.MultipartForm.File["evidence"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 evidence images allowed", http.StatusBadRequest)
			return
		}

		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
			return
		}

		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()

		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, files, "disputes/evidence")
		if err != nil {
			utils.WriteError(w, "failed to upload evidence images", http.StatusInternalServerError)
			return
		}
		for i, url := range urls {
			evidence = append(evidence, disputemodels.DisputeEvidence{
				URL:      url,
				PublicID: publicIDs[i],
			})
		}
	}

	evidenceJSON, _ := json.Marshal(evidence)

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var disputeID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO booking_disputes (booking_id, filed_by, respondent_id, reason, evidence, status)
		VALUES ($1, $2, $3, $4, $5, 'open')
		RETURNING id
	`, bookingID, userID, respondentID, reason, evidenceJSON).Scan(&disputeID)
	if err != nil {
		utils.Logger.Errorf("failed to create booking dispute: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `
		UPDATE artisan_bookings SET status = 'disputed', updated_at = NOW() WHERE id = $1
	`, bookingID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(
		context.Background(), respondentID, utils.NotifDisputeFiled,
		"Dispute Filed",
		fmt.Sprintf("A dispute has been filed against you for a booking. Reason: %s", reason),
		map[string]interface{}{"booking_id": bookingID, "dispute_id": disputeID},
	)
	dto.PushFallback(respondentID,
		"Dispute Filed",
		"A dispute has been filed against you. Payment is on hold pending review.")

	utils.WriteJSON(w, map[string]interface{}{
		"status":     "success",
		"message":    "dispute filed successfully. Payment is frozen pending resolution.",
		"dispute_id": disputeID,
	})
}

// ============================================================================
// POST /dispute-centre/orders/{id}/dispute  — client OR owner files an order (shortlet) dispute
// ============================================================================

// FileOrderDispute godoc
// @Summary      File an order dispute
// @Description  Allows a client or property owner to file a dispute for a shortlet order. The order must be confirmed, checked-in, or completed and must not already have an active dispute. Accepts multipart form data with an optional evidence field (max 5 images). Filing a dispute freezes the escrow funds pending admin resolution.
// @Tags         Disputes
// @Accept       multipart/form-data
// @Produce      json
// @Param        id        path      string  true   "Order UUID"
// @Param        reason    formData  string  true   "Reason for the dispute"
// @Param        evidence  formData  file    false  "Evidence images (max 5)"
// @Success      200  {object}  object{status=string,message=string,dispute_id=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Failure      409  {object}  object{error=string}
// @Router       /dispute-centre/orders/{id}/dispute [post]
// @Security     BearerAuth
func FileOrderDispute(w http.ResponseWriter, r *http.Request) {
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

	if role != "client" && role != "owner" {
		utils.WriteError(w, "only clients and owners can file order disputes", http.StatusForbidden)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid order id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := r.ParseMultipartForm(30 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	reason := r.FormValue("reason")
	if reason == "" {
		utils.WriteError(w, "reason is required", http.StatusBadRequest)
		return
	}

	var clientID, ownerID uuid.UUID
	var orderStatus string
	err = db.QueryRow(ctx, `
		SELECT client_id, owner_id, status FROM orders WHERE id = $1
	`, orderID).Scan(&clientID, &ownerID, &orderStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			utils.WriteError(w, "order not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var respondentID uuid.UUID
	switch role {
	case "client":
		if clientID != userID {
			utils.WriteError(w, "you did not place this order", http.StatusForbidden)
			return
		}
		respondentID = ownerID
	case "owner":
		if ownerID != userID {
			utils.WriteError(w, "you do not own the property for this order", http.StatusForbidden)
			return
		}
		respondentID = clientID
	}

	allowedStatuses := map[string]bool{
		"confirmed":  true,
		"checked_in": true,
		"completed":  true,
	}
	if !allowedStatuses[orderStatus] {
		utils.WriteError(w, "disputes can only be filed for confirmed, checked-in, or completed orders", http.StatusBadRequest)
		return
	}

	var existingCount int
	db.QueryRow(ctx, `
		SELECT COUNT(*) FROM order_disputes
		WHERE order_id = $1 AND status IN ('open','investigating')
	`, orderID).Scan(&existingCount)
	if existingCount > 0 {
		utils.WriteError(w, "an active dispute already exists for this order", http.StatusConflict)
		return
	}

	var evidence []disputemodels.DisputeEvidence

	if r.MultipartForm != nil && len(r.MultipartForm.File["evidence"]) > 0 {
		imageHeaders := r.MultipartForm.File["evidence"]
		if len(imageHeaders) > 5 {
			utils.WriteError(w, "maximum 5 evidence images allowed", http.StatusBadRequest)
			return
		}

		cloud, err := utils.InitCloudinary()
		if err != nil {
			utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
			return
		}

		var files []utils.UploadFile
		var openFiles []io.Closer
		for _, h := range imageHeaders {
			f, err := h.Open()
			if err != nil {
				continue
			}
			files = append(files, utils.UploadFile{Reader: f, Filename: h.Filename})
			openFiles = append(openFiles, f)
		}
		defer func() {
			for _, f := range openFiles {
				f.Close()
			}
		}()

		urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, files, "disputes/evidence")
		if err != nil {
			utils.WriteError(w, "failed to upload evidence images", http.StatusInternalServerError)
			return
		}
		for i, url := range urls {
			evidence = append(evidence, disputemodels.DisputeEvidence{
				URL:      url,
				PublicID: publicIDs[i],
			})
		}
	}

	evidenceJSON, _ := json.Marshal(evidence)

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var disputeID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO order_disputes (order_id, filed_by, respondent_id, reason, evidence, status)
		VALUES ($1, $2, $3, $4, $5, 'open')
		RETURNING id
	`, orderID, userID, respondentID, reason, evidenceJSON).Scan(&disputeID)
	if err != nil {
		utils.Logger.Errorf("failed to create order dispute: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `
		UPDATE orders SET status = 'disputed', updated_at = NOW() WHERE id = $1
	`, orderID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go utils.CreateNotification(
		context.Background(), respondentID, utils.NotifDisputeFiled,
		"Dispute Filed Against You",
		fmt.Sprintf("A dispute has been filed for an order. Reason: %s", reason),
		map[string]interface{}{"order_id": orderID, "dispute_id": disputeID},
	)
	dto.PushFallback(respondentID,
		"Dispute Filed",
		"A dispute has been filed against you. Payment is on hold pending review.")

	utils.WriteJSON(w, map[string]interface{}{
		"status":     "success",
		"message":    "dispute filed. Funds are frozen pending admin resolution.",
		"dispute_id": disputeID,
	})
}

// ============================================================================
// GET /dispute-centre/disputes/jobs  — user gets their job disputes
// ============================================================================

// GetMyJobDisputes godoc
// @Summary      List my job disputes
// @Description  Returns a paginated list of all job disputes filed by or against the authenticated user.
// @Tags         Disputes
// @Produce      json
// @Param        status  query     string  false  "Filter by status: open, investigating, resolved_refund, resolved_release, dismissed"
// @Param        page    query     int     false  "Page number (default: 1)"
// @Param        limit   query     int     false  "Items per page (default: 20)"
// @Success      200  {object}  object{status=string,count=int,data=array,pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      401  {object}  object{error=string}
// @Router       /dispute-centre/disputes/jobs [get]
// @Security     BearerAuth
func GetMyJobDisputes(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(r.Context(), `
		SELECT d.id, d.job_id, d.filed_by, d.respondent_id,
		       d.reason, d.evidence, d.status, d.admin_notes,
		       d.resolution, d.created_at, d.resolved_at,
		       j.status         AS job_status,
		       j.total_price,
		       filer.first_name || ' ' || filer.last_name AS filer_name,
		       resp.first_name  || ' ' || resp.last_name  AS respondent_name
		FROM job_disputes d
		JOIN jobs j          ON j.id = d.job_id
		JOIN users filer     ON filer.id = d.filed_by
		LEFT JOIN users resp ON resp.id = d.respondent_id
		WHERE d.filed_by = $1 OR d.respondent_id = $1
		ORDER BY d.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type DisputeRow struct {
		ID             uuid.UUID   `json:"id"`
		JobID          uuid.UUID   `json:"job_id"`
		FiledBy        uuid.UUID   `json:"filed_by"`
		FilerName      string      `json:"filer_name"`
		RespondentID   *uuid.UUID  `json:"respondent_id,omitempty"`
		RespondentName *string     `json:"respondent_name,omitempty"`
		Reason         string      `json:"reason"`
		Evidence       interface{} `json:"evidence"`
		Status         string      `json:"status"`
		AdminNotes     *string     `json:"admin_notes,omitempty"`
		Resolution     *string     `json:"resolution,omitempty"`
		JobStatus      string      `json:"job_status"`
		TotalPrice     *float64    `json:"total_price,omitempty"`
		CreatedAt      time.Time   `json:"created_at"`
		ResolvedAt     *time.Time  `json:"resolved_at,omitempty"`
	}

	disputes := make([]DisputeRow, 0)
	for rows.Next() {
		var d DisputeRow
		var evidenceJSON []byte
		if err := rows.Scan(
			&d.ID, &d.JobID, &d.FiledBy, &d.RespondentID,
			&d.Reason, &evidenceJSON, &d.Status, &d.AdminNotes,
			&d.Resolution, &d.CreatedAt, &d.ResolvedAt,
			&d.JobStatus, &d.TotalPrice,
			&d.FilerName, &d.RespondentName,
		); err != nil {
			continue
		}
		if len(evidenceJSON) > 0 {
			json.Unmarshal(evidenceJSON, &d.Evidence)
		}
		disputes = append(disputes, d)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(disputes),
		"data":   disputes,
	})
}

// ============================================================================
// GET /dispute-centre/disputes/bookings  — user gets their booking disputes
// ============================================================================

// GetMyBookingDisputes godoc
// @Summary      List my booking disputes
// @Description  Returns all booking disputes where the authenticated user is either the filer or the respondent, ordered by most recent. Supports pagination via page and limit query params.
// @Tags         Disputes
// @Produce      json
// @Param        page   query  int  false  "Page number (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=array}
// @Failure      401  {object}  object{error=string}
// @Router       /dispute-centre/disputes/bookings [get]
// @Security     BearerAuth
func GetMyBookingDisputes(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(r.Context(), `
		SELECT d.id, d.booking_id, d.filed_by, d.respondent_id,
		       d.reason, d.evidence, d.status, d.admin_notes,
		       d.resolution, d.created_at, d.resolved_at,
		       b.status         AS booking_status,
		       b.total_price,
		       b.booking_date::TEXT,
		       filer.first_name || ' ' || filer.last_name AS filer_name,
		       resp.first_name  || ' ' || resp.last_name  AS respondent_name
		FROM booking_disputes d
		JOIN artisan_bookings b ON b.id = d.booking_id
		JOIN users filer        ON filer.id = d.filed_by
		LEFT JOIN users resp    ON resp.id = d.respondent_id
		WHERE d.filed_by = $1 OR d.respondent_id = $1
		ORDER BY d.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type DisputeRow struct {
		ID             uuid.UUID   `json:"id"`
		BookingID      uuid.UUID   `json:"booking_id"`
		FiledBy        uuid.UUID   `json:"filed_by"`
		FilerName      string      `json:"filer_name"`
		RespondentID   uuid.UUID   `json:"respondent_id"`
		RespondentName *string     `json:"respondent_name,omitempty"`
		Reason         string      `json:"reason"`
		Evidence       interface{} `json:"evidence"`
		Status         string      `json:"status"`
		AdminNotes     *string     `json:"admin_notes,omitempty"`
		Resolution     *string     `json:"resolution,omitempty"`
		BookingStatus  string      `json:"booking_status"`
		TotalPrice     float64     `json:"total_price"`
		BookingDate    string      `json:"booking_date"`
		CreatedAt      time.Time   `json:"created_at"`
		ResolvedAt     *time.Time  `json:"resolved_at,omitempty"`
	}

	disputes := make([]DisputeRow, 0)
	for rows.Next() {
		var d DisputeRow
		var evidenceJSON []byte
		if err := rows.Scan(
			&d.ID, &d.BookingID, &d.FiledBy, &d.RespondentID,
			&d.Reason, &evidenceJSON, &d.Status, &d.AdminNotes,
			&d.Resolution, &d.CreatedAt, &d.ResolvedAt,
			&d.BookingStatus, &d.TotalPrice, &d.BookingDate,
			&d.FilerName, &d.RespondentName,
		); err != nil {
			continue
		}
		if len(evidenceJSON) > 0 {
			json.Unmarshal(evidenceJSON, &d.Evidence)
		}
		disputes = append(disputes, d)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(disputes),
		"data":   disputes,
	})
}

// ============================================================================
// GET /dispute-centre/disputes/orders  — user gets their order (shortlet) disputes
// ============================================================================

// GetMyOrderDisputes godoc
// @Summary      List my order disputes
// @Description  Returns all shortlet order disputes where the authenticated user is either the filer or the respondent, ordered by most recent. Supports pagination via page and limit query params.
// @Tags         Disputes
// @Produce      json
// @Param        page   query  int  false  "Page number (default 1)"
// @Param        limit  query  int  false  "Items per page (default 20)"
// @Success      200  {object}  object{status=string,count=int,data=array}
// @Failure      401  {object}  object{error=string}
// @Router       /dispute-centre/disputes/orders [get]
// @Security     BearerAuth
func GetMyOrderDisputes(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query(r.Context(), `
		SELECT d.id, d.order_id, d.filed_by, d.respondent_id,
		       d.reason, d.evidence, d.status, d.admin_notes,
		       d.resolution, d.created_at, d.resolved_at,
		       o.status        AS order_status,
		       o.total_amount,
		       o.check_in_date::TEXT,
		       o.check_out_date::TEXT,
		       filer.first_name || ' ' || filer.last_name AS filer_name,
		       resp.first_name  || ' ' || resp.last_name  AS respondent_name
		FROM order_disputes d
		JOIN orders o          ON o.id = d.order_id
		JOIN users filer       ON filer.id = d.filed_by
		LEFT JOIN users resp   ON resp.id = d.respondent_id
		WHERE d.filed_by = $1 OR d.respondent_id = $1
		ORDER BY d.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type DisputeRow struct {
		ID             uuid.UUID   `json:"id"`
		OrderID        uuid.UUID   `json:"order_id"`
		FiledBy        uuid.UUID   `json:"filed_by"`
		FilerName      string      `json:"filer_name"`
		RespondentID   uuid.UUID   `json:"respondent_id"`
		RespondentName *string     `json:"respondent_name,omitempty"`
		Reason         string      `json:"reason"`
		Evidence       interface{} `json:"evidence"`
		Status         string      `json:"status"`
		AdminNotes     *string     `json:"admin_notes,omitempty"`
		Resolution     *string     `json:"resolution,omitempty"`
		OrderStatus    string      `json:"order_status"`
		TotalAmount    float64     `json:"total_amount"`
		CheckInDate    string      `json:"check_in_date"`
		CheckOutDate   string      `json:"check_out_date"`
		CreatedAt      time.Time   `json:"created_at"`
		ResolvedAt     *time.Time  `json:"resolved_at,omitempty"`
	}

	disputes := make([]DisputeRow, 0)
	for rows.Next() {
		var d DisputeRow
		var evidenceJSON []byte
		if err := rows.Scan(
			&d.ID, &d.OrderID, &d.FiledBy, &d.RespondentID,
			&d.Reason, &evidenceJSON, &d.Status, &d.AdminNotes,
			&d.Resolution, &d.CreatedAt, &d.ResolvedAt,
			&d.OrderStatus, &d.TotalAmount, &d.CheckInDate, &d.CheckOutDate,
			&d.FilerName, &d.RespondentName,
		); err != nil {
			continue
		}
		if len(evidenceJSON) > 0 {
			json.Unmarshal(evidenceJSON, &d.Evidence)
		}
		disputes = append(disputes, d)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"count":  len(disputes),
		"data":   disputes,
	})
}
