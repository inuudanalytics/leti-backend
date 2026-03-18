package adminModels

import (
	"time"

	"github.com/google/uuid"
)

// ─── Admin ────────────────────────────────────────────────────────────────────

type AdminRole string

const (
	RoleSuperAdmin AdminRole = "super_admin"
	RoleAdmin      AdminRole = "admin"
	RoleSupport    AdminRole = "support"
)

type Admin struct {
	ID                uuid.UUID  `json:"id,omitempty"                  db:"id"`
	FullName          string     `json:"full_name,omitempty"           db:"full_name"`
	Email             string     `json:"email,omitempty"               db:"email"`
	Password          string     `json:"password,omitempty"            db:"password"`
	Role              AdminRole  `json:"role,omitempty"                db:"role"`
	IsActive          bool       `json:"is_active"                     db:"is_active"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"       db:"last_login_at"`
	PasswordChangedAt *time.Time `json:"password_changed_at,omitempty" db:"password_changed_at"`
	CreatedAt         time.Time  `json:"created_at,omitempty"          db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at,omitempty"          db:"updated_at"`
	CreatedBy         *uuid.UUID `json:"created_by,omitempty"          db:"created_by"`
}

// ─── Audit log ────────────────────────────────────────────────────────────────

type AuditLog struct {
	ID         uuid.UUID              `json:"id"`
	AdminID    uuid.UUID              `json:"admin_id"`
	AdminName  string                 `json:"admin_name,omitempty"`
	Action     string                 `json:"action"`
	EntityType string                 `json:"entity_type,omitempty"`
	EntityID   *uuid.UUID             `json:"entity_id,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	IPAddress  string                 `json:"ip_address,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

// ─── Platform settings ────────────────────────────────────────────────────────

type PlatformSetting struct {
	Key         string     `json:"key"`
	Value       string     `json:"value"`
	Description string     `json:"description,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ─── Request / response DTOs ──────────────────────────────────────────────────

type AdminLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type CreateAdminRequest struct {
	FullName string    `json:"full_name"`
	Email    string    `json:"email"`
	Password string    `json:"password"`
	Role     AdminRole `json:"role"`
}

type UpdateAdminRequest struct {
	FullName string    `json:"full_name,omitempty"`
	Role     AdminRole `json:"role,omitempty"`
	IsActive *bool     `json:"is_active,omitempty"`
}

type UpdateAdminPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type UpdateUserStatusRequest struct {
	Status string `json:"status"` // approved | suspended | probation | rejected | pending
	Reason string `json:"reason,omitempty"`
}

type UpdateSettingRequest struct {
	Value string `json:"value"`
}

// ─── Dashboard stats ──────────────────────────────────────────────────────────

type DashboardStats struct {
	TotalUsers        int     `json:"total_users"`
	TotalArtisans     int     `json:"total_artisans"`
	TotalOwners       int     `json:"total_owners"`
	TotalClients      int     `json:"total_clients"`
	ActiveUsers       int     `json:"active_users"`
	SuspendedUsers    int     `json:"suspended_users"`
	TotalBookings     int     `json:"total_bookings"`
	CompletedBookings int     `json:"completed_bookings"`
	PendingBookings   int     `json:"pending_bookings"`
	TotalJobs         int     `json:"total_jobs"`
	CompletedJobs     int     `json:"completed_jobs"`
	PendingJobs       int     `json:"pending_jobs"`
	TotalWalletVolume float64 `json:"total_wallet_volume_ngn"`
	NewUsersToday     int     `json:"new_users_today"`
	NewUsersThisWeek  int     `json:"new_users_this_week"`
	NewUsersThisMonth int     `json:"new_users_this_month"`
}

// ─── Paginated list helpers ───────────────────────────────────────────────────

type PaginatedResponse struct {
	Status  string      `json:"status"`
	Data    interface{} `json:"data"`
	Total   int         `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
	Pages   int         `json:"pages"`
}