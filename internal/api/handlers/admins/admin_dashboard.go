package admins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	adminModels "leti_server/internal/models/admins"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// GET /admin/dashboard
// ============================================================================

// AdminDashboardHandler godoc
// @Summary      Dashboard stats
// @Description  Returns aggregate platform statistics: user counts by role, booking counts, job counts, wallet volume, and new user trends.
// @Tags         Admin Dashboard
// @Produce      json
// @Success      200  {object}  object{status=string,data=adminModels.DashboardStats}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/dashboard [get]
// @Security     BearerAuth
func AdminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var stats adminModels.DashboardStats

	// User counts by role
	err := db.QueryRow(ctx, `
		SELECT
			COUNT(*)                                                    AS total,
			COUNT(*) FILTER (WHERE active_role = 'artisan')            AS artisans,
			COUNT(*) FILTER (WHERE active_role = 'owner')              AS owners,
			COUNT(*) FILTER (WHERE active_role = 'client')             AS clients,
			COUNT(*) FILTER (WHERE status = 'approved')                AS active,
			COUNT(*) FILTER (WHERE status = 'suspended')               AS suspended,
			COUNT(*) FILTER (WHERE user_created_at::date = CURRENT_DATE)                  AS today,
			COUNT(*) FILTER (WHERE user_created_at >= CURRENT_DATE - INTERVAL '7 days')   AS week,
			COUNT(*) FILTER (WHERE user_created_at >= date_trunc('month', CURRENT_DATE))  AS month
		FROM users
		WHERE deleted_at IS NULL
	`).Scan(
		&stats.TotalUsers,
		&stats.TotalArtisans,
		&stats.TotalOwners,
		&stats.TotalClients,
		&stats.ActiveUsers,
		&stats.SuspendedUsers,
		&stats.NewUsersToday,
		&stats.NewUsersThisWeek,
		&stats.NewUsersThisMonth,
	)
	if err != nil {
		utils.Logger.Errorf("dashboard user stats error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Booking counts
	_ = db.QueryRow(ctx, `
		SELECT
			COUNT(*)                                          AS total,
			COUNT(*) FILTER (WHERE status = 'completed')     AS completed,
			COUNT(*) FILTER (WHERE status = 'pending')       AS pending
		FROM bookings
	`).Scan(&stats.TotalBookings, &stats.CompletedBookings, &stats.PendingBookings)

	// Job counts
	_ = db.QueryRow(ctx, `
		SELECT
			COUNT(*)                                          AS total,
			COUNT(*) FILTER (WHERE status = 'completed')     AS completed,
			COUNT(*) FILTER (WHERE status = 'pending')       AS pending
		FROM jobs
	`).Scan(&stats.TotalJobs, &stats.CompletedJobs, &stats.PendingJobs)

	// Wallet volume
	_ = db.QueryRow(ctx, `SELECT COALESCE(SUM(balance), 0) FROM wallets`).
		Scan(&stats.TotalWalletVolume)

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "data": stats})
}

// ============================================================================
// GET /admin/dashboard/card
// ============================================================================

// AdminDashboardCardHandler godoc
// @Summary      Dashboard quick cards
// @Description  Returns live card metrics: total users, active artisans online, active bookings, total escrow funds held, and open disputes.
// @Tags         Admin Dashboard
// @Produce      json
// @Success      200  {object}  object{status=string,data=object{total_users=int,active_artisans=int,active_bookings=int,total_escrow_funds=number,total_open_disputes=int}}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/dashboard/card [get]
// @Security     BearerAuth
func AdminDashboardCardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var totalUsers, activeArtisans, activeBookings, openDisputes int
	var totalEscrowFunds float64

	err := db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL)                                      AS total_users,
			COUNT(*) FILTER (WHERE active_role = 'artisan' AND status = 'approved'
			                   AND is_online = TRUE AND deleted_at IS NULL)                 AS active_artisans,
			(SELECT COUNT(*) FROM bookings WHERE status IN ('confirmed','checked_in'))       AS active_bookings,
			(SELECT COALESCE(SUM(amount), 0) FROM escrows WHERE status = 'held')            AS total_escrow_funds,
			(SELECT COUNT(*) FROM disputes WHERE status IN ('open','investigating'))         AS open_disputes
		FROM users
	`).Scan(&totalUsers, &activeArtisans, &activeBookings, &totalEscrowFunds, &openDisputes)
	if err != nil {
		utils.Logger.Errorf("admin dashboard card query error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"total_users":         totalUsers,
			"active_artisans":     activeArtisans,
			"active_bookings":     activeBookings,
			"total_escrow_funds":  totalEscrowFunds,
			"total_open_disputes": openDisputes,
		},
	})
}

// ============================================================================
// GET /admin/dashboard/jobs-overview
// ============================================================================

// AdminJobsOverviewHandler godoc
// @Summary      Artisan jobs overview
// @Description  Returns job stats (active, completed, cancelled, disputed) and a paginated job list. Optionally filter by status.
// @Tags         Admin Dashboard
// @Produce      json
// @Param        page      query  int     false  "Page number"
// @Param        per_page  query  int     false  "Items per page"
// @Param        status    query  string  false  "Filter by job status"
// @Success      200  {object}  object{status=string,stats=object{},jobs=object{}}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/dashboard/jobs-overview [get]
// @Security     BearerAuth
func AdminJobsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var activeJobs, completedJobs, cancelledJobs, disputedJobs int
	_ = db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('accepted','in_progress'))  AS active,
			COUNT(*) FILTER (WHERE status = 'completed')                  AS completed,
			COUNT(*) FILTER (WHERE status = 'cancelled')                  AS cancelled,
			COUNT(*) FILTER (WHERE status = 'disputed')                   AS disputed
		FROM jobs
	`).Scan(&activeJobs, &completedJobs, &cancelledJobs, &disputedJobs)

	page, perPage := handlers.ParsePagination(r)
	filterStatus := r.URL.Query().Get("status")

	conditions := []string{"1=1"}
	args := []interface{}{}
	idx := 1

	if filterStatus != "" {
		conditions = append(conditions, "j.status = $"+handlers.Itoa(idx))
		args = append(args, filterStatus)
		idx++
	}

	where := strings.Join(conditions, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM jobs j WHERE "+where, countArgs...).Scan(&total)

	args = append(args, perPage, (page-1)*perPage)

	rows, err := db.Query(ctx, `
		SELECT
			j.id, j.status, j.created_at, j.completed_at,
			client.id, client.first_name, client.last_name, client.email,
			artisan.id, artisan.first_name, artisan.last_name, artisan.email,
			q.amount
		FROM jobs j
		JOIN users client ON j.client_id = client.id
		LEFT JOIN users artisan ON j.artisan_id = artisan.id
		LEFT JOIN quotations q ON q.job_id = j.id AND q.status = 'accepted'
		WHERE `+where+`
		ORDER BY j.created_at DESC
		LIMIT $`+handlers.Itoa(idx)+` OFFSET $`+handlers.Itoa(idx+1),
		args...,
	)
	if err != nil {
		utils.Logger.Errorf("admin jobs list error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Person struct {
		ID        string  `json:"id"`
		FirstName string  `json:"first_name"`
		LastName  string  `json:"last_name"`
		Email     *string `json:"email,omitempty"`
	}

	type JobItem struct {
		ID           string     `json:"id"`
		Status       string     `json:"status"`
		AgreedAmount *float64   `json:"agreed_amount,omitempty"`
		Client       Person     `json:"client"`
		Artisan      *Person    `json:"artisan,omitempty"`
		CreatedAt    time.Time  `json:"created_at"`
		CompletedAt  *time.Time `json:"completed_at,omitempty"`
	}

	var jobs []JobItem
	for rows.Next() {
		var j JobItem
		var artisanID, artisanFirst, artisanLast *string
		var artisanEmail *string
		if err := rows.Scan(
			&j.ID, &j.Status, &j.CreatedAt, &j.CompletedAt,
			&j.Client.ID, &j.Client.FirstName, &j.Client.LastName, &j.Client.Email,
			&artisanID, &artisanFirst, &artisanLast, &artisanEmail,
			&j.AgreedAmount,
		); err != nil {
			utils.Logger.Errorf("admin jobs scan error: %v", err)
			continue
		}
		if artisanID != nil {
			j.Artisan = &Person{
				ID:        *artisanID,
				FirstName: *artisanFirst,
				LastName:  *artisanLast,
				Email:     artisanEmail,
			}
		}
		jobs = append(jobs, j)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"stats": map[string]int{
			"active":    activeJobs,
			"completed": completedJobs,
			"cancelled": cancelledJobs,
			"disputed":  disputedJobs,
		},
		"jobs": handlers.BuildPaginatedResponse(jobs, total, page, perPage),
	})
}

// ============================================================================
// GET /admin/dashboard/jobs/{id}
// ============================================================================

// AdminGetJobHandler godoc
// @Summary      Get job detail
// @Description  Returns full detail for a single artisan job including the timeline of all events (requests, quotes, disputes).
// @Tags         Admin Dashboard
// @Produce      json
// @Param        id   path  string  true  "Job UUID"
// @Success      200  {object}  object{status=string,data=object{job=object{},timeline=[]object{},dispute=object{}}}
// @Failure      400  {object}  object{error=string}
// @Failure      403  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /admin/dashboard/jobs/{id} [get]
// @Security     BearerAuth
func AdminGetJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin", "support"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	jobID := handlers.ParsePathParam(r, "id")
	if jobID == "" {
		utils.WriteError(w, "job id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	type JobDetail struct {
		ID           string     `json:"id"`
		Status       string     `json:"status"`
		Description  *string    `json:"description,omitempty"`
		AgreedAmount *float64   `json:"agreed_amount,omitempty"`
		CreatedAt    time.Time  `json:"created_at"`
		CompletedAt  *time.Time `json:"completed_at,omitempty"`
		ClientID     string     `json:"client_id"`
		ClientFirst  string     `json:"client_first_name"`
		ClientLast   string     `json:"client_last_name"`
		ClientEmail  *string    `json:"client_email,omitempty"`
		ArtisanID    *string    `json:"artisan_id,omitempty"`
		ArtisanFirst *string    `json:"artisan_first_name,omitempty"`
		ArtisanLast  *string    `json:"artisan_last_name,omitempty"`
		ArtisanEmail *string    `json:"artisan_email,omitempty"`
	}

	var j JobDetail

	err := db.QueryRow(ctx, `
		SELECT
			j.id, j.status, j.description, j.created_at, j.completed_at,
			client.id, client.first_name, client.last_name, client.email,
			artisan.id, artisan.first_name, artisan.last_name, artisan.email,
			q.amount
		FROM jobs j
		JOIN users client ON j.client_id = client.id
		LEFT JOIN users artisan ON j.artisan_id = artisan.id
		LEFT JOIN quotations q ON q.job_id = j.id AND q.status = 'accepted'
		WHERE j.id = $1::uuid
	`, jobID).Scan(
		&j.ID, &j.Status, &j.Description, &j.CreatedAt, &j.CompletedAt,
		&j.ClientID, &j.ClientFirst, &j.ClientLast, &j.ClientEmail,
		&j.ArtisanID, &j.ArtisanFirst, &j.ArtisanLast, &j.ArtisanEmail,
		&j.AgreedAmount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "job not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("admin get job error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type TimelineEvent struct {
		Stage     string    `json:"stage"`
		Status    string    `json:"status"`
		Actor     *string   `json:"actor,omitempty"`
		ActorRole *string   `json:"actor_role,omitempty"`
		Amount    *float64  `json:"amount,omitempty"`
		OccuredAt time.Time `json:"occurred_at"`
	}

	var timeline []TimelineEvent

	clientName := j.ClientFirst + " " + j.ClientLast
	clientRole := "client"
	timeline = append(timeline, TimelineEvent{
		Stage:     "job_created",
		Status:    "pending",
		Actor:     &clientName,
		ActorRole: &clientRole,
		OccuredAt: j.CreatedAt,
	})

	// Quote events
	quotRows, err := db.Query(ctx, `
		SELECT q.amount, q.status, q.created_at, q.responded_at,
		       a.first_name, a.last_name, c.first_name, c.last_name
		FROM quotations q
		JOIN users a ON q.artisan_id  = a.id
		JOIN users c ON q.client_id   = c.id
		WHERE q.job_id = $1::uuid
		ORDER BY q.created_at ASC
	`, jobID)
	if err != nil {
		utils.Logger.Errorf("admin job timeline quotations error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer quotRows.Close()

	for quotRows.Next() {
		var amount float64
		var qStatus, aFirst, aLast, cFirst, cLast string
		var createdAt time.Time
		var respondedAt *time.Time
		if err := quotRows.Scan(&amount, &qStatus, &createdAt, &respondedAt, &aFirst, &aLast, &cFirst, &cLast); err != nil {
			continue
		}
		artisanName := aFirst + " " + aLast
		artisanRole := "artisan"
		timeline = append(timeline, TimelineEvent{
			Stage:     "quote_sent",
			Status:    "pending",
			Actor:     &artisanName,
			ActorRole: &artisanRole,
			Amount:    &amount,
			OccuredAt: createdAt,
		})
		if respondedAt != nil {
			cName := cFirst + " " + cLast
			cRole := "client"
			timeline = append(timeline, TimelineEvent{
				Stage:     "quote_" + qStatus,
				Status:    qStatus,
				Actor:     &cName,
				ActorRole: &cRole,
				Amount:    &amount,
				OccuredAt: *respondedAt,
			})
		}
	}

	if j.CompletedAt != nil {
		timeline = append(timeline, TimelineEvent{
			Stage:     "job_completed",
			Status:    "completed",
			OccuredAt: *j.CompletedAt,
		})
	}

	// Dispute
	type DisputeInfo struct {
		ID         string     `json:"id"`
		FiledBy    string     `json:"filed_by"`
		Reason     string     `json:"reason"`
		Status     string     `json:"status"`
		AdminNotes *string    `json:"admin_notes,omitempty"`
		CreatedAt  time.Time  `json:"created_at"`
		ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	}

	var dispute *DisputeInfo
	var d DisputeInfo
	var filerFirst, filerLast string

	err = db.QueryRow(ctx, `
		SELECT dp.id, u.first_name, u.last_name, dp.reason,
		       dp.status, dp.admin_notes, dp.created_at, dp.resolved_at
		FROM disputes dp
		JOIN users u ON dp.filed_by = u.id
		WHERE dp.job_id = $1::uuid
		LIMIT 1
	`, jobID).Scan(&d.ID, &filerFirst, &filerLast, &d.Reason, &d.Status, &d.AdminNotes, &d.CreatedAt, &d.ResolvedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		utils.Logger.Errorf("admin job dispute error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err == nil {
		filerName := filerFirst + " " + filerLast
		d.FiledBy = filerName
		dispute = &d
		timeline = append(timeline, TimelineEvent{
			Stage:     "dispute_filed",
			Status:    d.Status,
			Actor:     &filerName,
			OccuredAt: d.CreatedAt,
		})
		if d.ResolvedAt != nil {
			timeline = append(timeline, TimelineEvent{
				Stage:     "dispute_resolved",
				Status:    d.Status,
				OccuredAt: *d.ResolvedAt,
			})
		}
	}

	sort.Slice(timeline, func(i, k int) bool {
		return timeline[i].OccuredAt.Before(timeline[k].OccuredAt)
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"job":      j,
			"timeline": timeline,
			"dispute":  dispute,
		},
	})
}

// ============================================================================
// GET /admin/audit-logs
// ============================================================================

// AdminListAuditLogsHandler godoc
// @Summary      List audit logs
// @Description  Returns a paginated list of admin audit logs. Filterable by admin_id, action keyword, and entity_type.
// @Tags         Admin Audit
// @Produce      json
// @Param        page        query  int     false  "Page number"
// @Param        per_page    query  int     false  "Items per page"
// @Param        admin_id    query  string  false  "Filter by admin UUID"
// @Param        action      query  string  false  "Filter by action keyword (partial match)"
// @Param        entity_type query  string  false  "Filter by entity type e.g. user, booking, job"
// @Success      200  {object}  adminModels.PaginatedResponse
// @Failure      403  {object}  object{error=string}
// @Router       /admin/audit-logs [get]
// @Security     BearerAuth
func AdminListAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin"); err != nil {
		utils.WriteError(w, "forbidden: super_admin only", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	page, perPage := handlers.ParsePagination(r)
	filterAdminID := r.URL.Query().Get("admin_id")
	filterAction := r.URL.Query().Get("action")
	filterEntity := r.URL.Query().Get("entity_type")

	conditions := []string{"1=1"}
	args := []interface{}{}
	idx := 1

	if filterAdminID != "" {
		conditions = append(conditions, "l.admin_id = $"+handlers.Itoa(idx)+"::uuid")
		args = append(args, filterAdminID)
		idx++
	}
	if filterAction != "" {
		conditions = append(conditions, "l.action ILIKE $"+handlers.Itoa(idx))
		args = append(args, "%"+filterAction+"%")
		idx++
	}
	if filterEntity != "" {
		conditions = append(conditions, "l.entity_type = $"+handlers.Itoa(idx))
		args = append(args, filterEntity)
		idx++
	}

	where := strings.Join(conditions, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_logs l WHERE `+where, countArgs...).Scan(&total)

	args = append(args, perPage, (page-1)*perPage)

	rows, err := db.Query(ctx, `
		SELECT l.id, l.admin_id, a.full_name, l.action,
		       l.entity_type, l.entity_id, l.metadata, l.ip_address, l.created_at
		FROM admin_audit_logs l
		JOIN admins a ON l.admin_id = a.id
		WHERE `+where+`
		ORDER BY l.created_at DESC
		LIMIT $`+handlers.Itoa(idx)+` OFFSET $`+handlers.Itoa(idx+1),
		args...,
	)
	if err != nil {
		utils.Logger.Errorf("list audit logs error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []adminModels.AuditLog
	for rows.Next() {
		var l adminModels.AuditLog
		var metaBytes []byte
		if err := rows.Scan(
			&l.ID, &l.AdminID, &l.AdminName, &l.Action,
			&l.EntityType, &l.EntityID, &metaBytes, &l.IPAddress, &l.CreatedAt,
		); err != nil {
			continue
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &l.Metadata)
		}
		logs = append(logs, l)
	}

	utils.WriteJSON(w, handlers.BuildPaginatedResponse(logs, total, page, perPage))
}

// ============================================================================
// GET /admin/settings
// ============================================================================

// AdminListSettingsHandler godoc
// @Summary      List platform settings
// @Description  Returns all platform configuration settings.
// @Tags         Admin Settings
// @Produce      json
// @Success      200  {object}  object{status=string,data=[]adminModels.PlatformSetting}
// @Failure      403  {object}  object{error=string}
// @Router       /admin/settings [get]
// @Security     BearerAuth
func AdminListSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `SELECT key, value, description, updated_by, updated_at FROM platform_settings ORDER BY key`)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var settings []adminModels.PlatformSetting
	for rows.Next() {
		var s adminModels.PlatformSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.Description, &s.UpdatedBy, &s.UpdatedAt); err != nil {
			continue
		}
		settings = append(settings, s)
	}

	utils.WriteJSON(w, map[string]interface{}{"status": "success", "data": settings})
}

// ============================================================================
// PATCH /admin/settings/{key}
// ============================================================================

// AdminUpdateSettingHandler godoc
// @Summary      Update platform setting
// @Description  Updates the value of a platform configuration key. Super admin only.
// @Tags         Admin Settings
// @Accept       json
// @Produce      json
// @Param        key   path  string                          true  "Setting key"
// @Param        body  body  adminModels.UpdateSettingRequest  true  "New value"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /admin/settings/{key} [patch]
// @Security     BearerAuth
func AdminUpdateSettingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin"); err != nil {
		utils.WriteError(w, "forbidden: super_admin only", http.StatusForbidden)
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

	key := handlers.ParsePathParam(r, "key")
	if key == "" {
		utils.WriteError(w, "setting key is required", http.StatusBadRequest)
		return
	}

	var req adminModels.UpdateSettingRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Value == "" {
		utils.WriteError(w, "value is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var oldValue string
	err := db.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = $1`, key).Scan(&oldValue)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "setting not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx,
		`UPDATE platform_settings SET value = $1, updated_by = $2, updated_at = NOW() WHERE key = $3`,
		req.Value, callerID, key,
	)
	if err != nil {
		utils.Logger.Errorf("update setting error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	logAudit(ctx, db, callerID, "setting.update", "setting", nil, map[string]interface{}{
		"key": key, "old_value": oldValue, "new_value": req.Value,
	}, r)

	utils.WriteJSON(w, map[string]string{"status": "success", "message": "setting updated successfully"})
}

// ============================================================================
// GET /admin/jobs  (simple paginated list)
// ============================================================================

// AdminListJobsHandler godoc
// @Summary      List jobs
// @Description  Returns a paginated list of artisan jobs. Filterable by status, artisan_id, and client_id.
// @Tags         Admin Jobs
// @Produce      json
// @Param        page       query  int     false  "Page number"
// @Param        per_page   query  int     false  "Items per page"
// @Param        status     query  string  false  "Filter by job status"
// @Param        artisan_id query  string  false  "Filter by artisan UUID"
// @Param        client_id  query  string  false  "Filter by client UUID"
// @Success      200  {object}  adminModels.PaginatedResponse
// @Failure      403  {object}  object{error=string}
// @Router       /admin/jobs [get]
// @Security     BearerAuth
func AdminListJobsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := utils.AuthorizeUser(r.Context().Value(utils.ContextKey("role")).(string), "super_admin", "admin", "support"); err != nil {
		utils.WriteError(w, "forbidden", http.StatusForbidden)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	page, perPage := handlers.ParsePagination(r)
	status := r.URL.Query().Get("status")
	artisanID := r.URL.Query().Get("artisan_id")
	clientID := r.URL.Query().Get("client_id")

	conditions := []string{"1=1"}
	args := []interface{}{}
	idx := 1

	if status != "" {
		conditions = append(conditions, "j.status = $"+handlers.Itoa(idx))
		args = append(args, status)
		idx++
	}
	if artisanID != "" {
		conditions = append(conditions, "j.artisan_id = $"+handlers.Itoa(idx)+"::uuid")
		args = append(args, artisanID)
		idx++
	}
	if clientID != "" {
		conditions = append(conditions, "j.client_id = $"+handlers.Itoa(idx)+"::uuid")
		args = append(args, clientID)
		idx++
	}

	where := strings.Join(conditions, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM jobs j WHERE "+where, countArgs...).Scan(&total)

	args = append(args, perPage, (page-1)*perPage)

	rows, err := db.Query(ctx, `
		SELECT j.id, j.status, j.created_at, j.completed_at,
		       client.first_name, client.last_name,
		       artisan.first_name, artisan.last_name
		FROM jobs j
		LEFT JOIN users client ON j.client_id = client.id
		LEFT JOIN users artisan ON j.artisan_id = artisan.id
		WHERE `+where+`
		ORDER BY j.created_at DESC
		LIMIT $`+handlers.Itoa(idx)+` OFFSET $`+handlers.Itoa(idx+1),
		args...,
	)
	if err != nil {
		utils.Logger.Errorf("admin list jobs error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type jobRow struct {
		ID          string     `json:"id"`
		Status      string     `json:"status"`
		ClientName  string     `json:"client_name"`
		ArtisanName *string    `json:"artisan_name,omitempty"`
		CreatedAt   time.Time  `json:"created_at"`
		CompletedAt *time.Time `json:"completed_at,omitempty"`
	}

	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		var aFirst, aLast *string
		var cFirst, cLast string
		if err := rows.Scan(
			&j.ID, &j.Status, &j.CreatedAt, &j.CompletedAt,
			&cFirst, &cLast, &aFirst, &aLast,
		); err != nil {
			continue
		}
		j.ClientName = cFirst + " " + cLast
		if aFirst != nil {
			name := *aFirst + " " + *aLast
			j.ArtisanName = &name
		}
		jobs = append(jobs, j)
	}

	utils.WriteJSON(w, handlers.BuildPaginatedResponse(jobs, total, page, perPage))
}
