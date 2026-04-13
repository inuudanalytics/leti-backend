package routers

import (
	"leti_server/internal/api/handlers/admins"
	"net/http"
)

func adminRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Public (no auth) ─────────────────────────────────────────────────────
	mux.HandleFunc("POST /admin/auth/login", admins.AdminLoginHandler)
	mux.HandleFunc("POST /admin/auth/logout", admins.AdminLogoutHandler)
	mux.HandleFunc("POST /admin/auth/refresh", admins.AdminRefreshTokenHandler)

	// ── Admin auth ───────────────────────────────────────────────────────────
	mux.Handle("GET /admin/auth/me", http.HandlerFunc(admins.AdminGetMeHandler))
	mux.Handle("PATCH /admin/auth/password", http.HandlerFunc(admins.AdminUpdatePasswordHandler))

	// ── Admin management (super_admin only) ──────────────────────────────────
	mux.Handle("POST /admin/admins", http.HandlerFunc(admins.CreateAdminHandler))
	mux.Handle("GET /admin/admins", http.HandlerFunc(admins.ListAdminsHandler))
	mux.Handle("PATCH /admin/admins/{id}", http.HandlerFunc(admins.UpdateAdminHandler))

	// ── Dashboard ─────────────────────────────────────────────────────────────
	mux.Handle("GET /admin/dashboard", http.HandlerFunc(admins.AdminDashboardHandler))
	mux.Handle("GET /admin/dashboard/card", http.HandlerFunc(admins.AdminDashboardCardHandler))
	mux.Handle("GET /admin/dashboard/jobs-overview", http.HandlerFunc(admins.AdminJobsOverviewHandler))
	mux.Handle("GET /admin/dashboard/jobs/{id}", http.HandlerFunc(admins.AdminGetJobHandler))

	// ── Audit logs ────────────────────────────────────────────────────────────
	mux.Handle("GET /admin/audit-logs", http.HandlerFunc(admins.AdminListAuditLogsHandler))

	// ── Settings ──────────────────────────────────────────────────────────────
	mux.Handle("GET /admin/settings", http.HandlerFunc(admins.AdminListSettingsHandler))
	mux.Handle("PATCH /admin/settings/{key}", http.HandlerFunc(admins.AdminUpdateSettingHandler))

	// ── Users ─────────────────────────────────────────────────────────────────
	mux.Handle("GET /admin/users", http.HandlerFunc(admins.AdminListUsersHandler))
	mux.Handle("GET /admin/users/{id}", http.HandlerFunc(admins.AdminGetUserHandler))
	mux.Handle("PATCH /admin/users/{id}/status", http.HandlerFunc(admins.AdminUpdateUserStatusHandler))
	mux.Handle("DELETE /admin/users/{id}", http.HandlerFunc(admins.AdminDeleteUserHandler))

	// ── Jobs ──────────────────────────────────────────────────────────────────
	mux.Handle("GET /admin/jobs", http.HandlerFunc(admins.AdminListJobsHandler))

	// ── Finance / withdrawals ─────────────────────────────────────────────────
	mux.Handle("GET /admin/withdrawals", http.HandlerFunc(admins.AdminListWithdrawalsHandler))
	mux.Handle("GET /admin/withdrawals/{id}/verify", http.HandlerFunc(admins.AdminVerifyWithdrawalTransferHandler))

	// ── Devices ───────────────────────────────────────────────────────────────
	mux.Handle("POST /admin/device/tokens", http.HandlerFunc(admins.RegisterAdminDevice))

	// ── Support ─────────────────────────────────────────────────────────
	mux.HandleFunc("GET /admin/support/ws", admins.ServeAdminSupportWS)
	mux.HandleFunc("GET /admin/support", admins.AdminListSupportTickets)
	mux.HandleFunc("GET /admin/support/{ticketId}", admins.AdminGetSupportTicket)
	mux.HandleFunc("GET /admin/support/{ticketId}/messages", admins.AdminGetTicketMessages)
	mux.HandleFunc("POST /admin/support/{ticketId}/reply", admins.AdminReplySupportTicket)
	mux.HandleFunc("POST /admin/support/{ticketId}/reply/images", admins.AdminReplySupportTicketImage)
	mux.HandleFunc("PATCH /admin/support/{ticketId}/status", admins.AdminUpdateTicketStatus)
	mux.HandleFunc("PATCH /admin/support/{ticketId}/assign", admins.AdminAssignTicket)

	// ── List & detail ──────────────────────
	mux.Handle("GET /admin/disputes", http.HandlerFunc(admins.AdminListDisputes))
	mux.Handle("GET /admin/disputes/{id}", http.HandlerFunc(admins.AdminGetDispute))

	// ── Status update ──────────────────────
	mux.Handle("PATCH /admin/disputes/{id}/status", http.HandlerFunc(admins.AdminUpdateDisputeStatus))

	// ── Resolution decision ───────────────────
	mux.Handle("POST /admin/disputes/{id}/decision", http.HandlerFunc(admins.AdminResolveDispute))

	// ── Dispute ↔ support ticket ──────────────
	mux.Handle("POST /admin/disputes/{id}/ticket", http.HandlerFunc(admins.AdminCreateDisputeTicket))
	mux.Handle("GET /admin/disputes/{id}/ticket", http.HandlerFunc(admins.AdminGetDisputeTicket))

	return mux
}
