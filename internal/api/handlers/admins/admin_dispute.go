package admins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	disputemodels "leti_server/internal/models/support"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// GET /api/v1/admin/disputes
// Query params: type=job|booking|order, status, page, limit
// ============================================================================

// AdminListDisputes godoc
// @Summary      List all disputes (admin)
// @Description  Returns a paginated list of disputes across all types (job, booking, order) with aggregate stats. Filter by type and/or status.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        type    query     string  false  "Dispute type: job, booking, order (omit for all)"
// @Param        status  query     string  false  "Filter by status: open, investigating, resolved_refund, resolved_release, dismissed"
// @Param        page    query     int     false  "Page number (default: 1)"
// @Param        limit   query     int     false  "Items per page (default: 20)"
// @Param        limit   query     int     false  "type=job|booking|order, status, page, limit"
// @Success      200  {object}  object{status=string,stats=object{open=int,investigating=int,resolved=int},count=int,data=array,pagination=object{total=int,page=int,limit=int,total_pages=int}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/disputes [get]
// @Security     BearerAuth
func AdminListDisputes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	disputeType := r.URL.Query().Get("type") // "job" | "booking" | "order" | ""
	statusFilter := r.URL.Query().Get("status")

	page, limit := utils.GetPaginationParams(r)
	offset := (page - 1) * limit

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	type DisputeRow struct {
		ID         uuid.UUID  `json:"id"`
		Type       string     `json:"type"`
		RefID      uuid.UUID  `json:"ref_id"`
		FiledBy    uuid.UUID  `json:"filed_by"`
		FiledName  string     `json:"filed_by_name"`
		Respondent *uuid.UUID `json:"respondent_id,omitempty"`
		Reason     string     `json:"reason"`
		Status     string     `json:"status"`
		CreatedAt  time.Time  `json:"created_at"`
		ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	}

	var allRows []DisputeRow
	var totalCount int

	fetchDisputes := func(table, refCol, typeLabel string) {
		args := []interface{}{}
		where := []string{"1=1"}
		idx := 1

		if statusFilter != "" {
			where = append(where, fmt.Sprintf("d.status = $%d", idx))
			args = append(args, statusFilter)
			idx++
		}

		whereStr := strings.Join(where, " AND ")

		var cnt int
		db.QueryRow(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM %s d WHERE %s`, table, whereStr,
		), args...).Scan(&cnt)
		totalCount += cnt

		// Only apply limit/offset when fetching a single type
		q := fmt.Sprintf(`
			SELECT d.id, d.%s, d.filed_by,
			       u.first_name || ' ' || u.last_name,
			       d.respondent_id, d.reason, d.status,
			       d.created_at, d.resolved_at
			FROM %s d
			JOIN users u ON u.id = d.filed_by
			WHERE %s
			ORDER BY d.created_at DESC
		`, refCol, table, whereStr)

		if disputeType == typeLabel {
			args = append(args, limit, offset)
			q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", idx, idx+1)
		}

		rows, err := db.Query(ctx, q, args...)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var d DisputeRow
			d.Type = typeLabel
			rows.Scan(&d.ID, &d.RefID, &d.FiledBy, &d.FiledName,
				&d.Respondent, &d.Reason, &d.Status, &d.CreatedAt, &d.ResolvedAt)
			allRows = append(allRows, d)
		}
	}

	if disputeType == "" || disputeType == "job" {
		fetchDisputes("job_disputes", "job_id", "job")
	}
	if disputeType == "" || disputeType == "booking" {
		fetchDisputes("booking_disputes", "booking_id", "booking")
	}
	if disputeType == "" || disputeType == "order" {
		fetchDisputes("order_disputes", "order_id", "order")
	}

	// When fetching all types, slice to page window
	if disputeType == "" && len(allRows) > 0 {
		start := offset
		if start > len(allRows) {
			start = len(allRows)
		}
		end := start + limit
		if end > len(allRows) {
			end = len(allRows)
		}
		allRows = allRows[start:end]
	}

	// Stats
	var openCount, investigatingCount, resolvedCount int
	for _, tbl := range []string{"job_disputes", "booking_disputes", "order_disputes"} {
		var n int
		db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = 'open'`, tbl)).Scan(&n)
		openCount += n
		db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = 'investigating'`, tbl)).Scan(&n)
		investigatingCount += n
		db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status LIKE 'resolved%%'`, tbl)).Scan(&n)
		resolvedCount += n
	}

	totalPages := (totalCount + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"stats": map[string]int{
			"open":          openCount,
			"investigating": investigatingCount,
			"resolved":      resolvedCount,
		},
		"count": len(allRows),
		"data":  allRows,
		"pagination": map[string]int{
			"total":       totalCount,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ============================================================================
// GET /api/v1/admin/disputes/{id}?type=job|booking|order
// ============================================================================

// AdminGetDispute godoc
// @Summary      Get a single dispute (admin)
// @Description  Returns full detail for a single dispute including parties, reason, evidence, escrow snapshot, admin notes, and resolution. Supply the dispute type via the `type` query param.
// @Tags         Admin / Disputes
// @Produce      json
// @Param        id    path      string  true   "Dispute UUID"
// @Param        type  query     string  false  "Dispute type: job, booking, or order (default: job)"
// @Success      200  {object}  object{status=string,type=string,data=object}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/disputes/{id} [get]
// @Security     BearerAuth
func AdminGetDispute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	disputeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid dispute id", http.StatusBadRequest)
		return
	}

	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	switch disputeType {
	case "job":
		var d struct {
			ID             uuid.UUID  `json:"id"`
			JobID          uuid.UUID  `json:"job_id"`
			FiledBy        uuid.UUID  `json:"filed_by"`
			FiledName      string     `json:"filed_by_name"`
			FiledRole      string     `json:"filed_by_role"`
			RespondentID   *uuid.UUID `json:"respondent_id,omitempty"`
			RespondentName *string    `json:"respondent_name,omitempty"`
			Reason         string     `json:"reason"`
			Evidence       *string    `json:"evidence,omitempty"`
			Status         string     `json:"status"`
			AdminNotes     *string    `json:"admin_notes,omitempty"`
			Resolution     *string    `json:"resolution,omitempty"`
			CreatedAt      time.Time  `json:"created_at"`
			ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
			EscrowAmount   *float64   `json:"escrow_amount,omitempty"`
			EscrowStatus   *string    `json:"escrow_status,omitempty"`
		}
		err = db.QueryRow(ctx, `
			SELECT jd.id, jd.job_id, jd.filed_by, u1.first_name || ' ' || u1.last_name, u1.active_role,
			       jd.respondent_id, u2.first_name || ' ' || u2.last_name,
			       jd.reason, jd.evidence::TEXT, jd.status,
			       jd.admin_notes, jd.resolution,
			       jd.created_at, jd.resolved_at,
			       e.amount, e.status
			FROM job_disputes jd
			JOIN users u1 ON u1.id = jd.filed_by
			LEFT JOIN users u2 ON u2.id = jd.respondent_id
			LEFT JOIN jobs_escrow e ON e.job_id = jd.job_id AND e.status = 'held'
			WHERE jd.id = $1
		`, disputeID).Scan(
			&d.ID, &d.JobID, &d.FiledBy, &d.FiledName, &d.FiledRole,
			&d.RespondentID, &d.RespondentName,
			&d.Reason, &d.Evidence, &d.Status,
			&d.AdminNotes, &d.Resolution,
			&d.CreatedAt, &d.ResolvedAt,
			&d.EscrowAmount, &d.EscrowStatus,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "dispute not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("admin get job dispute: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		utils.WriteJSON(w, map[string]interface{}{"status": "success", "type": "job", "data": d})

	case "booking":
		var d struct {
			ID             uuid.UUID  `json:"id"`
			BookingID      uuid.UUID  `json:"booking_id"`
			FiledBy        uuid.UUID  `json:"filed_by"`
			FiledName      string     `json:"filed_by_name"`
			FiledRole      string     `json:"filed_by_role"`
			RespondentID   *uuid.UUID `json:"respondent_id,omitempty"`
			RespondentName *string    `json:"respondent_name,omitempty"`
			Reason         string     `json:"reason"`
			Evidence       *string    `json:"evidence,omitempty"`
			Status         string     `json:"status"`
			AdminNotes     *string    `json:"admin_notes,omitempty"`
			Resolution     *string    `json:"resolution,omitempty"`
			CreatedAt      time.Time  `json:"created_at"`
			ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
			EscrowAmount   *float64   `json:"escrow_amount,omitempty"`
			NetPayout      *float64   `json:"escrow_net_payout,omitempty"`
			EscrowStatus   *string    `json:"escrow_status,omitempty"`
		}
		err = db.QueryRow(ctx, `
			SELECT bd.id, bd.booking_id, bd.filed_by, u1.first_name || ' ' || u1.last_name, u1.active_role,
			       bd.respondent_id, u2.first_name || ' ' || u2.last_name,
			       bd.reason, bd.evidence::TEXT, bd.status,
			       bd.admin_notes, bd.resolution,
			       bd.created_at, bd.resolved_at,
			       e.amount, e.net_payout, e.status
			FROM booking_disputes bd
			JOIN users u1 ON u1.id = bd.filed_by
			LEFT JOIN users u2 ON u2.id = bd.respondent_id
			LEFT JOIN booking_escrow e ON e.booking_id = bd.booking_id AND e.status = 'held'
			WHERE bd.id = $1
		`, disputeID).Scan(
			&d.ID, &d.BookingID, &d.FiledBy, &d.FiledName, &d.FiledRole,
			&d.RespondentID, &d.RespondentName,
			&d.Reason, &d.Evidence, &d.Status,
			&d.AdminNotes, &d.Resolution,
			&d.CreatedAt, &d.ResolvedAt,
			&d.EscrowAmount, &d.NetPayout, &d.EscrowStatus,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "dispute not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("admin get booking dispute: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		utils.WriteJSON(w, map[string]interface{}{"status": "success", "type": "booking", "data": d})

	case "order":
		var d struct {
			ID             uuid.UUID  `json:"id"`
			OrderID        uuid.UUID  `json:"order_id"`
			FiledBy        uuid.UUID  `json:"filed_by"`
			FiledName      string     `json:"filed_by_name"`
			FiledRole      string     `json:"filed_by_role"`
			RespondentID   *uuid.UUID `json:"respondent_id,omitempty"`
			RespondentName *string    `json:"respondent_name,omitempty"`
			Reason         string     `json:"reason"`
			Evidence       *string    `json:"evidence,omitempty"`
			Status         string     `json:"status"`
			AdminNotes     *string    `json:"admin_notes,omitempty"`
			Resolution     *string    `json:"resolution,omitempty"`
			CreatedAt      time.Time  `json:"created_at"`
			ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
			EscrowAmount   *float64   `json:"escrow_amount,omitempty"`
			NetPayout      *float64   `json:"escrow_net_payout,omitempty"`
			EscrowStatus   *string    `json:"escrow_status,omitempty"`
		}
		err = db.QueryRow(ctx, `
			SELECT od.id, od.order_id, od.filed_by, u1.first_name || ' ' || u1.last_name, u1.active_role,
			       od.respondent_id, u2.first_name || ' ' || u2.last_name,
			       od.reason, od.evidence::TEXT, od.status,
			       od.admin_notes, od.resolution,
			       od.created_at, od.resolved_at,
			       e.amount, e.net_payout, e.status
			FROM order_disputes od
			JOIN users u1 ON u1.id = od.filed_by
			LEFT JOIN users u2 ON u2.id = od.respondent_id
			LEFT JOIN order_escrow e ON e.order_id = od.order_id AND e.status = 'held'
			WHERE od.id = $1
		`, disputeID).Scan(
			&d.ID, &d.OrderID, &d.FiledBy, &d.FiledName, &d.FiledRole,
			&d.RespondentID, &d.RespondentName,
			&d.Reason, &d.Evidence, &d.Status,
			&d.AdminNotes, &d.Resolution,
			&d.CreatedAt, &d.ResolvedAt,
			&d.EscrowAmount, &d.NetPayout, &d.EscrowStatus,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				utils.WriteError(w, "dispute not found", http.StatusNotFound)
				return
			}
			utils.Logger.Errorf("admin get order dispute: %v", err)
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		utils.WriteJSON(w, map[string]interface{}{"status": "success", "type": "order", "data": d})

	default:
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
	}
}

// ============================================================================
// PATCH /api/v1/admin/disputes/{id}/status
// Body: { "status": "investigating"|"open", "admin_notes": "..." }
// ============================================================================

// AdminUpdateDisputeStatus godoc
// @Summary      Update a dispute status (admin)
// @Description  Sets the status of a dispute to `open` or `investigating` and optionally attaches admin notes. Use this to flag that a dispute is being looked into before issuing a final decision.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                        true   "Dispute UUID"
// @Param        type  query     string                        false  "Dispute type: job, booking, or order (default: job)"
// @Param        body  body      AdminUpdateDisputeStatusRequest  true   "Status update payload"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/disputes/{id}/status [patch]
// @Security     BearerAuth
func AdminUpdateDisputeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
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

	disputeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid dispute id", http.StatusBadRequest)
		return
	}

	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}

	type request struct {
		Status     string `json:"status"`
		AdminNotes string `json:"admin_notes"`
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	allowed := map[string]bool{"open": true, "investigating": true}
	if !allowed[req.Status] {
		utils.WriteError(w, "status must be 'open' or 'investigating'", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tableMap := map[string]string{
		"job":     "job_disputes",
		"booking": "booking_disputes",
		"order":   "order_disputes",
	}
	table, ok := tableMap[disputeType]
	if !ok {
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET status = $1, admin_notes = $2 WHERE id = $3
	`, table), req.Status, req.AdminNotes, disputeID)
	if err != nil {
		utils.Logger.Errorf("admin update dispute status: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "dispute not found", http.StatusNotFound)
		return
	}

	logAudit(ctx, db, callerID, "dispute.status_update", disputeType+"_dispute", &disputeID,
		map[string]interface{}{"new_status": req.Status, "notes": req.AdminNotes}, r)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "dispute status updated",
	})
}

// ============================================================================
// POST /api/v1/admin/disputes/{id}/decision
// Body: DisputeDecisionRequest
// ============================================================================

// AdminResolveDispute godoc
// @Summary      Resolve a dispute (admin)
// @Description  Issues a final decision on a dispute. Depending on the chosen action, funds held in escrow are refunded to the payer, released to the payee, or split. The dispute and parent record (job / booking / order) are updated accordingly and both parties are notified.
// @Tags         Admin / Disputes
// @Accept       json
// @Produce      json
// @Param        id    path      string                              true   "Dispute UUID"
// @Param        type  query     string                              false  "Dispute type: job, booking, or order (default: job)"
// @Param        body  body      support.DisputeDecisionRequest      true   "Decision payload"
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}  "Invalid action, amount, or dispute already resolved"
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/disputes/{id}/decision [post]
// @Security     BearerAuth
func AdminResolveDispute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerRole, _ := r.Context().Value(utils.ContextKey("role")).(string)
	if callerRole != "super_admin" && callerRole != "admin" {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
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

	disputeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		utils.WriteError(w, "invalid dispute id", http.StatusBadRequest)
		return
	}

	disputeType := r.URL.Query().Get("type")
	if disputeType == "" {
		disputeType = "job"
	}

	var req disputemodels.DisputeDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	validActions := map[string]bool{
		"refund_full": true, "release_full": true,
		"refund_partial": true, "dismiss": true,
	}
	if !validActions[req.Action] {
		utils.WriteError(w, "action must be one of: refund_full, release_full, refund_partial, dismiss", http.StatusBadRequest)
		return
	}
	if req.Action == "refund_partial" && req.Amount <= 0 {
		utils.WriteError(w, "amount is required for partial refund", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var resolveErr error
	switch disputeType {
	case "job":
		resolveErr = resolveJobDispute(ctx, db, disputeID, req, callerID, r)
	case "booking":
		resolveErr = resolveBookingDispute(ctx, db, disputeID, req, callerID, r)
	case "order":
		resolveErr = resolveOrderDispute(ctx, db, disputeID, req, callerID, r)
	default:
		utils.WriteError(w, "type must be one of: job, booking, order", http.StatusBadRequest)
		return
	}

	if resolveErr != nil {
		utils.Logger.Errorf("admin resolve %s dispute: %v", disputeType, resolveErr)
		utils.WriteError(w, resolveErr.Error(), http.StatusBadRequest)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("dispute resolved with action: %s", req.Action),
	})
}

// ── Job dispute resolution ────────────────────────────────────────────────────

func resolveJobDispute(
	ctx context.Context,
	_ interface{},
	disputeID uuid.UUID,
	req disputemodels.DisputeDecisionRequest,
	adminID uuid.UUID,
	r *http.Request,
) error {
	pool := sqlconnect.DB

	var jobID, filedBy uuid.UUID
	var respondentID *uuid.UUID
	var disputeStatus string
	err := pool.QueryRow(ctx, `
		SELECT job_id, filed_by, respondent_id, status FROM job_disputes WHERE id = $1
	`, disputeID).Scan(&jobID, &filedBy, &respondentID, &disputeStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("dispute not found")
		}
		return err
	}

	if disputeStatus == "resolved_refund" || disputeStatus == "resolved_release" || disputeStatus == "dismissed" {
		return fmt.Errorf("dispute is already resolved")
	}

	var escrowID uuid.UUID
	var escrowAmount float64
	var payerID, payeeID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT id, amount, payer_id, payee_id FROM jobs_escrow WHERE job_id = $1 AND status = 'held'
	`, jobID).Scan(&escrowID, &escrowAmount, &payerID, &payeeID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to fetch escrow: %v", err)
	}
	hasEscrow := err != pgx.ErrNoRows

	commissionRate := 0.08

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("internal server error")
	}
	defer tx.Rollback(ctx)

	resolvedStatus := "dismissed"

	if hasEscrow {
		switch req.Action {
		case "refund_full":
			resolvedStatus = "resolved_refund"
			tx.Exec(ctx, `UPDATE jobs_escrow SET status = 'refunded', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, escrowAmount, payerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				payerID, escrowAmount, escrowID)
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'waived', paid_at = NOW() WHERE job_id = $1 AND status = 'pending'`, jobID)

			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved — Full Refund",
				fmt.Sprintf("Your dispute has been resolved. ₦%.2f has been refunded to your wallet.", escrowAmount),
				map[string]interface{}{"dispute_id": disputeID, "job_id": jobID})
			if respondentID != nil {
				go utils.CreateNotification(context.Background(), *respondentID, utils.NotifDisputeResolved,
					"Dispute Resolved",
					"The dispute for your job has been resolved. The payment has been refunded to the client.",
					map[string]interface{}{"dispute_id": disputeID, "job_id": jobID})
			}

		case "release_full":
			resolvedStatus = "resolved_release"
			commission := escrowAmount * commissionRate
			artisanPayout := escrowAmount - commission

			tx.Exec(ctx, `UPDATE jobs_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, artisanPayout, payeeID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
				payeeID, artisanPayout, escrowID)
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'paid', paid_at = NOW() WHERE job_id = $1 AND status = 'pending'`, jobID)

			go utils.CreateNotification(context.Background(), payeeID, utils.NotifDisputeResolved,
				"Dispute Resolved — Payment Released",
				fmt.Sprintf("The dispute has been resolved in your favour. ₦%.2f has been released to your wallet.", artisanPayout),
				map[string]interface{}{"dispute_id": disputeID, "job_id": jobID})
			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved",
				"The dispute has been reviewed. The payment has been released to the artisan.",
				map[string]interface{}{"dispute_id": disputeID, "job_id": jobID})

		case "refund_partial":
			resolvedStatus = "resolved_refund"
			refundAmount := req.Amount
			if refundAmount > escrowAmount {
				tx.Rollback(ctx)
				return fmt.Errorf("refund amount (%.2f) exceeds escrow (%.2f)", refundAmount, escrowAmount)
			}
			releaseAmount := escrowAmount - refundAmount
			commission := releaseAmount * commissionRate
			artisanPayout := releaseAmount - commission

			tx.Exec(ctx, `UPDATE jobs_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, refundAmount, payerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				payerID, refundAmount, escrowID)
			if artisanPayout > 0 {
				tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, artisanPayout, payeeID)
				tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
					VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
					payeeID, artisanPayout, escrowID)
			}
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'paid', paid_at = NOW() WHERE job_id = $1 AND status = 'pending'`, jobID)

			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved — Partial Refund",
				fmt.Sprintf("Your dispute has been partially resolved. ₦%.2f refunded to your wallet.", refundAmount),
				map[string]interface{}{"dispute_id": disputeID})
			go utils.CreateNotification(context.Background(), payeeID, utils.NotifDisputeResolved,
				"Dispute Resolved — Partial Payment",
				fmt.Sprintf("The dispute has been resolved. ₦%.2f has been released to your wallet.", artisanPayout),
				map[string]interface{}{"dispute_id": disputeID})
		}
	}

	resolution := req.Resolution
	if resolution == "" {
		resolution = req.AdminNotes
	}
	tx.Exec(ctx, `
		UPDATE job_disputes
		SET status = $1, admin_notes = $2, resolution = $3, resolved_at = NOW()
		WHERE id = $4
	`, resolvedStatus, req.AdminNotes, resolution, disputeID)

	tx.Exec(ctx, `UPDATE jobs SET status = 'completed' WHERE id = $1 AND status = 'disputed'`, jobID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("internal server error: failed to commit")
	}

	logAudit(ctx, pool, adminID, "dispute.resolve", "job_dispute", &disputeID, map[string]interface{}{
		"action": req.Action, "amount": req.Amount, "resolution": resolution,
	}, r)

	return nil
}

// ── Booking dispute resolution ────────────────────────────────────────────────

func resolveBookingDispute(
	ctx context.Context,
	_ interface{},
	disputeID uuid.UUID,
	req disputemodels.DisputeDecisionRequest,
	adminID uuid.UUID,
	r *http.Request,
) error {
	pool := sqlconnect.DB

	var bookingID, filedBy, respondentID uuid.UUID
	var disputeStatus string
	err := pool.QueryRow(ctx, `
		SELECT booking_id, filed_by, respondent_id, status FROM booking_disputes WHERE id = $1
	`, disputeID).Scan(&bookingID, &filedBy, &respondentID, &disputeStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("dispute not found")
		}
		return err
	}

	if disputeStatus == "resolved_refund" || disputeStatus == "resolved_release" || disputeStatus == "dismissed" {
		return fmt.Errorf("dispute is already resolved")
	}

	var escrowID uuid.UUID
	var escrowAmount, commission, netPayout float64
	var payerID, payeeID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT id, amount, commission, net_payout, payer_id, payee_id
		FROM booking_escrow WHERE booking_id = $1 AND status = 'held'
	`, bookingID).Scan(&escrowID, &escrowAmount, &commission, &netPayout, &payerID, &payeeID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to fetch escrow: %v", err)
	}
	hasEscrow := err != pgx.ErrNoRows

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("internal server error")
	}
	defer tx.Rollback(ctx)

	resolvedStatus := "dismissed"

	if hasEscrow {
		switch req.Action {
		case "refund_full":
			resolvedStatus = "resolved_refund"
			tx.Exec(ctx, `UPDATE booking_escrow SET status = 'refunded', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, escrowAmount, payerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				payerID, escrowAmount, escrowID)
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'waived', paid_at = NOW() WHERE booking_id = $1 AND status = 'pending'`, bookingID)

			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved — Full Refund",
				fmt.Sprintf("Your booking dispute has been resolved. ₦%.2f refunded to your wallet.", escrowAmount),
				map[string]interface{}{"dispute_id": disputeID, "booking_id": bookingID})
			go utils.CreateNotification(context.Background(), respondentID, utils.NotifDisputeResolved,
				"Dispute Resolved",
				"The booking dispute has been resolved. Payment has been refunded to the client.",
				map[string]interface{}{"dispute_id": disputeID})

		case "release_full":
			resolvedStatus = "resolved_release"
			tx.Exec(ctx, `UPDATE booking_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, netPayout, payeeID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
				payeeID, netPayout, escrowID)
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'paid', paid_at = NOW() WHERE booking_id = $1 AND status = 'pending'`, bookingID)

			go utils.CreateNotification(context.Background(), payeeID, utils.NotifDisputeResolved,
				"Dispute Resolved — Payment Released",
				fmt.Sprintf("The booking dispute has been resolved in your favour. ₦%.2f released to your wallet.", netPayout),
				map[string]interface{}{"dispute_id": disputeID})
			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved",
				"The booking dispute has been reviewed. Payment has been released to the artisan.",
				map[string]interface{}{"dispute_id": disputeID})

		case "refund_partial":
			resolvedStatus = "resolved_refund"
			refundAmount := req.Amount
			if refundAmount > escrowAmount {
				tx.Rollback(ctx)
				return fmt.Errorf("refund amount exceeds escrow amount")
			}
			releaseAmount := escrowAmount - refundAmount
			commissionOnRelease := releaseAmount * 0.08
			artisanPayout := releaseAmount - commissionOnRelease

			tx.Exec(ctx, `UPDATE booking_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, refundAmount, payerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				payerID, refundAmount, escrowID)
			if artisanPayout > 0 {
				tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, artisanPayout, payeeID)
				tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
					VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
					payeeID, artisanPayout, escrowID)
			}
			tx.Exec(ctx, `UPDATE platform_commissions SET status = 'paid', paid_at = NOW() WHERE booking_id = $1 AND status = 'pending'`, bookingID)

			go utils.CreateNotification(context.Background(), payerID, utils.NotifDisputeResolved,
				"Dispute Resolved — Partial Refund",
				fmt.Sprintf("₦%.2f has been refunded to your wallet.", refundAmount),
				map[string]interface{}{"dispute_id": disputeID})
			go utils.CreateNotification(context.Background(), payeeID, utils.NotifDisputeResolved,
				"Dispute Resolved — Partial Release",
				fmt.Sprintf("₦%.2f has been released to your wallet.", artisanPayout),
				map[string]interface{}{"dispute_id": disputeID})
		}
	}

	resolution := req.Resolution
	if resolution == "" {
		resolution = req.AdminNotes
	}
	tx.Exec(ctx, `
		UPDATE booking_disputes
		SET status = $1, admin_notes = $2, resolution = $3, resolved_at = NOW()
		WHERE id = $4
	`, resolvedStatus, req.AdminNotes, resolution, disputeID)

	// Restore booking to completed
	tx.Exec(ctx, `
		UPDATE artisan_bookings SET status = 'completed', updated_at = NOW()
		WHERE id = $1 AND status = 'disputed'
	`, bookingID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("internal server error: failed to commit")
	}

	logAudit(ctx, pool, adminID, "dispute.resolve", "booking_dispute", &disputeID, map[string]interface{}{
		"action": req.Action, "amount": req.Amount, "resolution": resolution,
	}, r)

	return nil
}

// ── Order (shortlet) dispute resolution ──────────────────────────────────────

func resolveOrderDispute(
	ctx context.Context,
	_ interface{},
	disputeID uuid.UUID,
	req disputemodels.DisputeDecisionRequest,
	adminID uuid.UUID,
	r *http.Request,
) error {
	pool := sqlconnect.DB

	var orderID, filedBy, respondentID uuid.UUID
	var disputeStatus string
	err := pool.QueryRow(ctx, `
		SELECT order_id, filed_by, respondent_id, status FROM order_disputes WHERE id = $1
	`, disputeID).Scan(&orderID, &filedBy, &respondentID, &disputeStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("dispute not found")
		}
		return err
	}

	if disputeStatus == "resolved_refund" || disputeStatus == "resolved_release" || disputeStatus == "dismissed" {
		return fmt.Errorf("dispute is already resolved")
	}

	var escrowID uuid.UUID
	var escrowAmount, netPayout float64
	var buyerID, sellerID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT id, amount, net_payout, payer_id, payee_id
		FROM order_escrow WHERE order_id = $1 AND status = 'held'
	`, orderID).Scan(&escrowID, &escrowAmount, &netPayout, &buyerID, &sellerID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to fetch escrow: %v", err)
	}
	hasEscrow := err != pgx.ErrNoRows

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("internal server error")
	}
	defer tx.Rollback(ctx)

	resolvedStatus := "dismissed"

	if hasEscrow {
		switch req.Action {
		case "refund_full":
			resolvedStatus = "resolved_refund"
			tx.Exec(ctx, `UPDATE order_escrow SET status = 'refunded' WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, escrowAmount, buyerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				buyerID, escrowAmount, escrowID)

			go utils.CreateNotification(context.Background(), buyerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved — Full Refund",
				fmt.Sprintf("₦%.2f has been refunded to your wallet.", escrowAmount),
				map[string]interface{}{"dispute_id": disputeID, "order_id": orderID})
			go utils.CreateNotification(context.Background(), sellerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved",
				"The dispute has been reviewed. The payment has been refunded to the client.",
				map[string]interface{}{"dispute_id": disputeID})

		case "release_full":
			resolvedStatus = "resolved_release"
			tx.Exec(ctx, `UPDATE order_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, netPayout, sellerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
				sellerID, netPayout, escrowID)

			go utils.CreateNotification(context.Background(), sellerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved — Payment Released",
				fmt.Sprintf("₦%.2f has been released to your wallet.", netPayout),
				map[string]interface{}{"dispute_id": disputeID})
			go utils.CreateNotification(context.Background(), buyerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved",
				"The dispute has been reviewed. Payment was released to the owner.",
				map[string]interface{}{"dispute_id": disputeID})

		case "refund_partial":
			resolvedStatus = "resolved_refund"
			refundAmount := req.Amount
			if refundAmount > escrowAmount {
				tx.Rollback(ctx)
				return fmt.Errorf("refund amount exceeds escrow amount")
			}
			releaseAmount := escrowAmount - refundAmount
			tx.Exec(ctx, `UPDATE order_escrow SET status = 'released', released_at = NOW() WHERE id = $1`, escrowID)
			tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, refundAmount, buyerID)
			tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
				VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'refund', $3)`,
				buyerID, refundAmount, escrowID)
			if releaseAmount > 0 {
				tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1, last_transaction_at = NOW() WHERE user_id = $2`, releaseAmount, sellerID)
				tx.Exec(ctx, `INSERT INTO wallet_transactions (wallet_id, amount, type, reference_id)
					VALUES ((SELECT id FROM wallets WHERE user_id = $1), $2, 'escrow_release', $3)`,
					sellerID, releaseAmount, escrowID)
			}

			go utils.CreateNotification(context.Background(), buyerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved — Partial Refund",
				fmt.Sprintf("₦%.2f has been refunded to your wallet.", refundAmount),
				map[string]interface{}{"dispute_id": disputeID})
			go utils.CreateNotification(context.Background(), sellerID, utils.NotifDisputeResolved,
				"Order Dispute Resolved — Partial Release",
				fmt.Sprintf("₦%.2f has been released to your wallet.", releaseAmount),
				map[string]interface{}{"dispute_id": disputeID})
		}
	}

	resolution := req.Resolution
	if resolution == "" {
		resolution = req.AdminNotes
	}
	tx.Exec(ctx, `
		UPDATE order_disputes
		SET status = $1, admin_notes = $2, resolution = $3, resolved_at = NOW()
		WHERE id = $4
	`, resolvedStatus, req.AdminNotes, resolution, disputeID)

	// Restore order to completed
	tx.Exec(ctx, `
		UPDATE orders SET status = 'completed', updated_at = NOW()
		WHERE id = $1 AND status = 'disputed'
	`, orderID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("internal server error: failed to commit")
	}

	logAudit(ctx, pool, adminID, "dispute.resolve", "order_dispute", &disputeID, map[string]interface{}{
		"action": req.Action, "amount": req.Amount, "resolution": resolution,
	}, r)

	return nil
}
