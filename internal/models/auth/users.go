package authModels

import (
	"time"

	"github.com/google/uuid"
)

type UserAvatar struct {
	URL      string `json:"url"`
	PublicID string `json:"public_id"`
}

type User struct {
	ID                    uuid.UUID   `json:"id,omitempty"                     db:"id"`
	FirstName             string      `json:"first_name,omitempty"             db:"first_name"`
	LastName              string      `json:"last_name,omitempty"              db:"last_name"`
	Username              string      `json:"username,omitempty"               db:"username"`
	Email                 string      `json:"email,omitempty"                  db:"email"`
	PhoneNumber           string      `json:"phone_number,omitempty"           db:"phone_number"`
	Avatar                *UserAvatar `json:"avatar,omitempty"                 db:"avatar"`
	Password              string      `json:"password,omitempty"               db:"password"`
	PasswordChangedAt     *time.Time  `json:"password_changed_at,omitempty"    db:"password_changed_at"`
	UserCreatedAt         time.Time   `json:"user_created_at,omitempty"        db:"user_created_at"`
	PasswordResetToken    *string     `json:"password_reset_token,omitempty"   db:"password_reset_token"`
	PasswordTokenExpires  *time.Time  `json:"password_token_expires,omitempty" db:"password_token_expires"`
	Otp                   *string     `json:"otp,omitempty"                    db:"otp"`
	OtpExpires            *time.Time  `json:"otp_expires,omitempty"            db:"otp_expires"`
	IsOnline              *bool       `json:"is_online,omitempty"              db:"is_online"`
	RecoveryEmail         *string     `json:"recovery_email,omitempty"         db:"recovery_email"`
	RecoveryEmailVerified bool        `json:"recovery_email_verified,omitempty" db:"recovery_email_verified"`
	PhoneVerified         bool        `json:"phone_verified,omitempty"         db:"phone_verified"`
	EmailVerified         bool        `json:"email_verified,omitempty"         db:"email_verified"`
	Bio                   *string     `json:"bio,omitempty"`
	DeletedAt             *time.Time  `json:"deleted_at,omitempty"             db:"deleted_at"`
	Status                string      `json:"status,omitempty"                 db:"status"`
	ActiveRole            string      `json:"active_role,omitempty"            db:"active_role"`
	AuthProvider          string      `json:"auth_provider,omitempty"          db:"auth_provider"`
	GoogleSub             string      `json:"google_sub,omitempty"             db:"google_sub"`
	AppleSub              string      `json:"apple_sub,omitempty"              db:"apple_sub"`
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type SignupRequest struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Password    string `json:"password"`
	ActiveRole  string `json:"active_role"`
	OtpChannel  string `json:"otp_channel,omitempty"`

	ID                    uuid.UUID  `json:"-"`
	Otp                   *string    `json:"-"`
	OtpExpires            *time.Time `json:"-"`
	AuthProvider          string     `json:"-"`
	Status                string     `json:"-"`
	IsOnline              *bool      `json:"-"`
	RecoveryEmailVerified bool       `json:"-"`
	UserCreatedAt         time.Time  `json:"-"`
}
