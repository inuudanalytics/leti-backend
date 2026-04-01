package routers

import (
	"leti_server/internal/api/handlers/auth"
	profilesettings "leti_server/internal/api/handlers/profile_settings"
	mw "leti_server/internal/api/middlewares"
	"net/http"
	"time"
)

func usersRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// ── Rate limiters ─────────────────────────────────────────────────────────
	// Tight: OTP generation, login, signup, password reset — abuse-prone endpoints
	tightRL := mw.NewRateLimiter(5, time.Minute) // 5 req/min per IP
	// Moderate: token refresh, logout, general auth actions
	moderateRL := mw.NewRateLimiter(20, time.Minute) // 20 req/min per IP
	// Loose: read-only or low-risk protected endpoints
	looseRL := mw.NewRateLimiter(60, time.Minute) // 60 req/min per IP

	rl := func(limiter *mw.RateLimiter, h http.HandlerFunc) http.HandlerFunc {
		return limiter.Middleware(http.HandlerFunc(h)).ServeHTTP
	}

	// ── Auth (public) ─────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/signup", rl(tightRL, auth.RegisterUsersHandler))
	mux.HandleFunc("POST /auth/verify-otp", rl(tightRL, auth.ConfirmUsersOtpHandler))
	mux.HandleFunc("POST /auth/resend-otp", rl(tightRL, auth.ResendUsersOtpHandler))
	mux.HandleFunc("POST /auth/login", rl(tightRL, auth.LoginUserHandler))
	mux.HandleFunc("POST /auth/logout", rl(moderateRL, auth.LogoutUserHandler))
	mux.HandleFunc("POST /auth/forgot-password", rl(tightRL, auth.ForgotUserPasswordHandler))
	mux.HandleFunc("PATCH /auth/forgot-password/reset", rl(tightRL, auth.ResetUserPasswordHandler))
	mux.HandleFunc("POST /auth/google", rl(tightRL, auth.GoogleSignInHandler))
	mux.HandleFunc("POST /auth/apple", rl(tightRL, auth.AppleSignInHandler))
	mux.HandleFunc("POST /auth/refresh", rl(moderateRL, auth.RefreshTokenHandler))

	// ── Auth (protected) ──────────────────────────────────────────────────────
	mux.HandleFunc("PATCH /auth/password", rl(tightRL, auth.UpdateUserPasswordHandler))

	// ── User profile ──────────────────────────────────────────────────────────
	mux.HandleFunc("GET /auth/users/me", rl(looseRL, auth.GetUserMeHandler))
	mux.HandleFunc("PATCH /auth/users/me", rl(moderateRL, profilesettings.UpdateUserProfileHandler))
	mux.HandleFunc("PATCH /auth/users/switch-role", rl(moderateRL, auth.SwitchRoleHandler))
	mux.HandleFunc("POST /auth/users/avatar", rl(moderateRL, auth.UploadAvatar))
	mux.HandleFunc("DELETE /auth/users/account", rl(tightRL, auth.DeleteAccountHandler))
	mux.HandleFunc("GET /auth/users/contact-status", rl(looseRL, auth.GetContactStatusHandler))

	// ── Secondary contact ─────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/users/verify/add-contact", rl(tightRL, auth.AddSecondaryContactHandler))
	mux.HandleFunc("POST /auth/users/verify/confirm-contact", rl(tightRL, auth.ConfirmSecondaryContactHandler))

	// ── Recovery email ────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/users/recovery-email", rl(tightRL, auth.AddRecoveryEmailHandler))
	mux.HandleFunc("POST /auth/users/recovery-email/verify", rl(tightRL, auth.VerifyRecoveryEmailHandler))
	mux.HandleFunc("DELETE /auth/users/recovery-email", rl(moderateRL, auth.RemoveRecoveryEmailHandler))

	mux.HandleFunc("POST /auth/users/verify/request-contact-otp", rl(tightRL, auth.RequestUnverifiedContactOTPHandler))
	mux.HandleFunc("POST /auth/users/verify/confirm-contact-otp", rl(tightRL, auth.ConfirmUnverifiedContactOTPHandler))

	// ── Check username availability ────────────────────────────────────────────
	mux.HandleFunc("GET /auth/users/check-username", rl(looseRL, auth.CheckUsernameHandler))

	// ── Devices ───────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /auth/devices/register", rl(moderateRL, auth.RegisterDevice))

	return mux
}
