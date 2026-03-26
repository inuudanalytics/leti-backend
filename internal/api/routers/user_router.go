package routers

import (
	"leti_server/internal/api/handlers/auth"
	"net/http"
)

func usersRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Auth (public) ─────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/signup", auth.RegisterUsersHandler)
	mux.HandleFunc("POST /auth/verify-otp", auth.ConfirmUsersOtpHandler)
	mux.HandleFunc("POST /auth/resend-otp", auth.ResendUsersOtpHandler)
	mux.HandleFunc("POST /auth/login", auth.LoginUserHandler)
	mux.HandleFunc("POST /auth/logout", auth.LogoutUserHandler)
	mux.HandleFunc("POST /auth/forgot-password", auth.ForgotUserPasswordHandler)
	mux.HandleFunc("PATCH /auth/forgot-password/reset", auth.ResetUserPasswordHandler)
	mux.HandleFunc("POST /auth/google", auth.GoogleSignInHandler)
	mux.HandleFunc("POST /auth/apple", auth.AppleSignInHandler)
	mux.HandleFunc("POST /auth/refresh", auth.RefreshTokenHandler)

	// ── Auth (protected) ──────────────────────────────────────────────────────
	mux.HandleFunc("PATCH /auth/password", auth.UpdateUserPasswordHandler)

	// ── User profile ──────────────────────────────────────────────────────────
	mux.HandleFunc("GET /auth/users/me", auth.GetUserMeHandler)
	mux.HandleFunc("PATCH /auth/users/switch-role", auth.SwitchRoleHandler)
	mux.HandleFunc("POST /auth/users/avatar", auth.UploadAvatar)
	mux.HandleFunc("DELETE /auth/users/account", auth.DeleteAccountHandler)
	mux.HandleFunc("GET /auth/users/contact-status", auth.GetContactStatusHandler)

	// ── Secondary contact ─────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/users/verify/add-contact", auth.AddSecondaryContactHandler)
	mux.HandleFunc("POST /auth/users/verify/confirm-contact", auth.ConfirmSecondaryContactHandler)

	// ── Recovery email ────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/users/recovery-email", auth.AddRecoveryEmailHandler)
	mux.HandleFunc("POST /auth/users/recovery-email/verify", auth.VerifyRecoveryEmailHandler)
	mux.HandleFunc("DELETE /auth/users/recovery-email", auth.RemoveRecoveryEmailHandler)

	mux.HandleFunc("POST /auth/users/verify/request-contact-otp", auth.RequestUnverifiedContactOTPHandler)
	mux.HandleFunc("POST /auth/users/verify/confirm-contact-otp", auth.ConfirmUnverifiedContactOTPHandler)

	// ── Check username availability ────────────────────────────
	mux.HandleFunc("GET /auth/users/check-username", auth.CheckUsernameHandler)

	// ── Devices ───────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/devices/register", auth.RegisterDevice)

	return mux
}
