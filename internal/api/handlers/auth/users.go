package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"leti_server/internal/api/handlers"
	authModels "leti_server/internal/models/auth"
	walletModels "leti_server/internal/models/wallet"
	"leti_server/internal/repositories/sqlconnect"
	"leti_server/pkg/cache"
	"leti_server/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/argon2"
)

// ============================================================================
// POST /auth/signup
// ============================================================================

// RegisterUsersHandler godoc
// @Summary      Register a new user
// @Description  Creates a new user account and sends an OTP for verification. Wallet is created automatically on registration. Username is required and must be unique.
// @Description  If only email is provided, OTP is sent to email. If only phone_number is provided, OTP is sent to phone.
// @Description  If both email and phone_number are provided, a first request (without otp_channel) returns status "choose_channel" — resend the full payload with otp_channel set to either "email" or "phone".
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{first_name=string,last_name=string,username=string,email=string,phone_number=string,password=string,active_role=string,otp_channel=string}  true  "Registration payload. Provide email, phone_number, or both. If both, include otp_channel ('email' or 'phone'). active_role must be client, artisan, or owner"
// @Success      200   {object}  object{status=string,message=string,data=authModels.User,wallet=object{id=string,balance=number,currency=string,created_at=string}}
// @Success      200   {object}  object{status=string,message=string,options=[]string}  "status=choose_channel — returned when both email and phone are provided but otp_channel is missing"
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Failure      500   {object}  object{error=string}
// @Router       /auth/signup [post]
func RegisterUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.Logger.Error("DB is not initialized")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var req authModels.SignupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	hasPhone := req.PhoneNumber != ""
	hasEmail := req.Email != ""

	if !hasPhone && !hasEmail {
		utils.WriteError(w, "please provide either email or phone number", http.StatusBadRequest)
		return
	}

	if hasPhone && hasEmail {
		if req.OtpChannel == "" {
			utils.WriteJSON(w, map[string]interface{}{
				"status":  "choose_channel",
				"message": "please choose where to receive your OTP",
				"options": []string{"email", "phone"},
			})
			return
		}
		if req.OtpChannel != "email" && req.OtpChannel != "phone" {
			utils.WriteError(w, "otp_channel must be 'email' or 'phone'", http.StatusBadRequest)
			return
		}
	}

	if req.Password == "" {
		utils.WriteError(w, "please create a password", http.StatusBadRequest)
		return
	}
	if req.ActiveRole == "" {
		utils.WriteError(w, "please select a role: client, artisan, or owner", http.StatusBadRequest)
		return
	}
	if !handlers.AllowedRoles[req.ActiveRole] {
		utils.WriteError(w, "invalid role: must be client, artisan, or owner", http.StatusBadRequest)
		return
	}
	if req.FirstName == "" {
		utils.WriteError(w, "first name is required", http.StatusBadRequest)
		return
	}
	if req.LastName == "" {
		utils.WriteError(w, "last name is required", http.StatusBadRequest)
		return
	}
	if req.Username == "" {
		utils.WriteError(w, "username is required", http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(strings.ToLower(req.Username))
	if len(req.Username) < 3 {
		utils.WriteError(w, "username must be at least 3 characters", http.StatusBadRequest)
		return
	}
	if len(req.Username) > 50 {
		utils.WriteError(w, "username must be at most 50 characters", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber != "" {
		if err := utils.ValidateNigerianPhone(req.PhoneNumber); err != nil {
			utils.WriteError(w, "enter a valid phone number", http.StatusBadRequest)
			return
		}
	}
	if req.Email != "" {
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	{
		ctx2, cancel2 := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel2()
		available, err := handlers.IsUsernameAvailable(ctx2, req.Username)
		if err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !available {
			utils.WriteError(w, "username is already taken", http.StatusConflict)
			return
		}
	}

	duration, err := strconv.Atoi(os.Getenv("OTP_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.Logger.Error("failed to read OTP_TOKEN_EXP_DURATION")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.Logger.Errorf("failed to generate otp: %v", err)
		utils.WriteError(w, "failed to generate otp", http.StatusInternalServerError)
		return
	}

	req.Otp = &otp
	req.OtpExpires = &expiryTime
	req.AuthProvider = "local"
	req.Status = handlers.RegistrationStatus(req.ActiveRole)
	req.RecoveryEmailVerified = false

	if req.ActiveRole == "artisan" {
		isOnline := false
		req.IsOnline = &isOnline
	}

	hashedPwd, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.WriteError(w, "internal server error, please try again later", http.StatusInternalServerError)
		return
	}
	req.Password = hashedPwd

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.Logger.Errorf("failed to start transaction: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var existingID uuid.UUID
	var checkQuery, checkParam string

	switch {
	case hasEmail && hasPhone && req.OtpChannel == "email":
		checkQuery = `SELECT id FROM users WHERE email = $1 AND email_verified = FALSE AND phone_verified = FALSE`
		checkParam = req.Email
	case hasEmail && hasPhone && req.OtpChannel == "phone":
		checkQuery = `SELECT id FROM users WHERE phone_number = $1 AND phone_verified = FALSE AND email_verified = FALSE`
		checkParam = req.PhoneNumber
	case hasEmail:
		checkQuery = `SELECT id FROM users WHERE email = $1 AND email_verified = FALSE AND phone_verified = FALSE`
		checkParam = req.Email
	default:
		checkQuery = `SELECT id FROM users WHERE phone_number = $1 AND phone_verified = FALSE AND email_verified = FALSE`
		checkParam = req.PhoneNumber
	}

	checkErr := tx.QueryRow(ctx, checkQuery, checkParam).Scan(&existingID)
	existingUnverified := checkErr == nil

	var userID uuid.UUID
	var userCreatedAt time.Time

	if existingUnverified {
		err = tx.QueryRow(ctx, `
			UPDATE users
			SET first_name    = $1,
			    last_name     = $2,
			    username      = $3,
			    password      = $4,
			    active_role   = $5,
			    otp           = $6,
			    otp_expires   = $7,
			    status        = $8,
			    auth_provider = $9,
			    is_online     = $10
			WHERE id = $11
			RETURNING id, user_created_at
		`,
			req.FirstName,
			req.LastName,
			req.Username,
			req.Password,
			req.ActiveRole,
			req.Otp,
			req.OtpExpires,
			req.Status,
			req.AuthProvider,
			req.IsOnline,
			existingID,
		).Scan(&userID, &userCreatedAt)
		if err != nil {
			utils.Logger.Errorf("failed to update stale unverified user: %v", err)
			utils.WriteError(w, "error signing up", http.StatusInternalServerError)
			return
		}
	} else {
		err = tx.QueryRow(ctx, `
			INSERT INTO users (
				first_name, last_name, username, email, phone_number, password,
				active_role, status, auth_provider, is_online, otp, otp_expires
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			RETURNING id, user_created_at
		`,
			req.FirstName,
			req.LastName,
			req.Username,
			handlers.NullableString(req.Email),
			handlers.NullableString(req.PhoneNumber),
			req.Password,
			req.ActiveRole,
			req.Status,
			req.AuthProvider,
			req.IsOnline,
			req.Otp,
			req.OtpExpires,
		).Scan(&userID, &userCreatedAt)
		if err != nil {
			if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
				if strings.Contains(pgErr.Message, "username") {
					utils.WriteError(w, "username is already taken", http.StatusConflict)
				} else {
					utils.WriteError(w, "email or phone number already exists", http.StatusConflict)
				}
				return
			}
			utils.Logger.Errorf("failed to insert user: %v", err)
			utils.WriteError(w, "error signing up", http.StatusInternalServerError)
			return
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role) VALUES ($1, $2)
		ON CONFLICT (user_id, role) DO NOTHING
	`, userID, req.ActiveRole)
	if err != nil {
		utils.Logger.Errorf("failed to insert user_role: %v", err)
		utils.WriteError(w, "error signing up", http.StatusInternalServerError)
		return
	}

	var walletID uuid.UUID
	var walletCreatedAt time.Time

	err = tx.QueryRow(ctx, `
		INSERT INTO wallets (user_id, balance, currency, is_active)
		VALUES ($1, 0.00, 'NGN', TRUE)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING id, wallet_created_at
	`, userID).Scan(&walletID, &walletCreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id, wallet_created_at FROM wallets WHERE user_id = $1`, userID).
				Scan(&walletID, &walletCreatedAt)
			if err != nil {
				utils.Logger.Errorf("failed to fetch existing wallet: %v", err)
				utils.WriteError(w, "error fetching wallet", http.StatusInternalServerError)
				return
			}
		} else {
			utils.Logger.Errorf("failed to create wallet: %v", err)
			utils.WriteError(w, "error creating user wallet", http.StatusInternalServerError)
			return
		}
	}

	if err = tx.Commit(ctx); err != nil {
		utils.Logger.Errorf("failed to commit transaction: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	go cache.UsernameBloomAdd(context.Background(), req.Username)

	displayName := req.FirstName + " " + req.LastName
	var otpDestination string

	sendToEmail := hasEmail && (!hasPhone || req.OtpChannel == "email")
	sendToPhone := hasPhone && (!hasEmail || req.OtpChannel == "phone")

	if sendToEmail {
		otpDestination = "email"
		go func(email, name, otp string, expiry time.Time) {
			if err := utils.SendOTPEmail(email, name, otp, expiry); err != nil {
				utils.Logger.Errorf("failed to send OTP email to %s: %v", email, err)
			}
		}(req.Email, displayName, otp, expiryTime)
	} else if sendToPhone {
		otpDestination = "phone number"
		go func(phone, name, otp string, expiry time.Time) {
			if err := utils.SendOTPSMS(phone, name, otp, expiry); err != nil {
				utils.Logger.Errorf("failed to send OTP SMS to %s: %v", phone, err)
			}
		}(req.PhoneNumber, displayName, otp, expiryTime)
	}

	responseUser := authModels.User{
		ID:            userID,
		FirstName:     req.FirstName,
		LastName:      req.LastName,
		Username:      req.Username,
		Email:         req.Email,
		PhoneNumber:   req.PhoneNumber,
		ActiveRole:    req.ActiveRole,
		Status:        req.Status,
		AuthProvider:  req.AuthProvider,
		IsOnline:      req.IsOnline,
		UserCreatedAt: userCreatedAt,
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("OTP sent to your %s for verification", otpDestination),
		"data":    responseUser,
		"wallet": map[string]interface{}{
			"id":         walletID,
			"balance":    0.00,
			"currency":   "NGN",
			"created_at": walletCreatedAt,
		},
	})
}

// ============================================================================
// POST /auth/verify-otp
// ============================================================================

// ConfirmUsersOtpHandler godoc
// @Summary      Verify OTP
// @Description  Verifies the OTP sent to the user's email or phone number during registration. On success, issues a JWT and sends a role-specific welcome email.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{otp=string}  true  "OTP code"
// @Success      200   {object}  object{status=string,message=string,token=string,user=object{id=string,first_name=string,last_name=string,username=string,email=string,phone_number=string,status=string,active_role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      500   {object}  object{error=string}
// @Router       /auth/verify-otp [post]
func ConfirmUsersOtpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		Otp string `json:"otp"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Otp == "" {
		utils.WriteError(w, "please enter otp", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var user authModels.User
	var email sql.NullString
	var phone sql.NullString

	err := db.QueryRow(ctx,
		`SELECT id, first_name, last_name, username, email, status, phone_number, active_role
		 FROM users WHERE otp = $1 AND otp_expires > $2`,
		req.Otp, time.Now().Format(time.RFC3339),
	).Scan(&user.ID, &user.FirstName, &user.LastName, &user.Username, &email, &user.Status, &phone, &user.ActiveRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired otp", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if email.Valid {
		user.Email = email.String
	}
	if phone.Valid {
		user.PhoneNumber = phone.String
	}

	hasEmail := user.Email != ""
	hasPhone := user.PhoneNumber != ""

	var updateQuery string
	if hasEmail {
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, email_verified = TRUE WHERE id = $1"
	} else if hasPhone {
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, phone_verified = TRUE WHERE id = $1"
	} else {
		utils.Logger.Error("user has neither email nor phone number")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, updateQuery, user.ID)
	if err != nil {
		utils.Logger.Errorf("failed to verify otp: %v", err)
		utils.WriteError(w, "could not verify otp", http.StatusInternalServerError)
		return
	}

	displayName := user.FirstName + " " + user.LastName

	if hasEmail {
		go func(email, name, role string) {
			var sendErr error
			switch role {
			case "client":
				sendErr = utils.SendWelcomeEmailClient(email, name)
			case "owner":
				sendErr = utils.SendWelcomeEmailOwner(email, name)
			case "artisan":
				sendErr = utils.SendWelcomeEmailArtisan(email, name)
			}
			if sendErr != nil {
				utils.Logger.Errorf("failed to send welcome email to %s: %v", email, sendErr)
			}
		}(user.Email, displayName, user.ActiveRole)
	}

	tokenString, err := utils.SignToken(user.ID, displayName, user.ActiveRole)
	if err != nil {
		utils.Logger.Errorf("could not create login token: %v", err)
		utils.WriteError(w, "verification successful but failed to login", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.Logger.Errorf("failed to generate refresh token: %v", err)
		utils.WriteError(w, "verification successful but failed to create session", http.StatusInternalServerError)
		return
	}
	if err := utils.StoreRefreshToken(ctx, user.ID, refreshToken, ""); err != nil {
		utils.Logger.Errorf("failed to store refresh token: %v", err)
		utils.WriteError(w, "verification successful but failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(15 * time.Minute),
		SameSite: http.SameSiteNoneMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		SameSite: http.SameSiteStrictMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "OTP verified successfully, welcome to Leti!",
		"token":   tokenString,
		"user": map[string]interface{}{
			"id":           user.ID,
			"first_name":   user.FirstName,
			"last_name":    user.LastName,
			"username":     user.Username,
			"email":        user.Email,
			"status":       user.Status,
			"phone_number": user.PhoneNumber,
			"active_role":  user.ActiveRole,
		},
	})
}

// ============================================================================
// POST /auth/resend-otp
// ============================================================================

// ResendUsersOtpHandler godoc
// @Summary      Resend OTP
// @Description  Resends a new OTP to the user's email or phone number. Only works if the contact is not yet verified.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{email=string,phone_number=string}  true  "Provide either email or phone_number"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      404   {object}  object{error=string}
// @Router       /auth/resend-otp [post]
func ResendUsersOtpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		PhoneNumber string `json:"phone_number"`
		Email       string `json:"email"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	hasPhone := req.PhoneNumber != ""
	hasEmail := req.Email != ""

	if hasPhone == hasEmail {
		utils.WriteError(w, "please provide either email or phone number", http.StatusBadRequest)
		return
	}
	if hasPhone {
		if err := utils.ValidateNigerianPhone(req.PhoneNumber); err != nil {
			utils.WriteError(w, "enter a valid phone number", http.StatusBadRequest)
			return
		}
	}
	if hasEmail {
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var user authModels.User
	var isVerified bool
	var emailNull, phoneNull sql.NullString
	var query, queryParam string

	if hasEmail {
		query = `SELECT id, first_name, last_name, email, phone_number, email_verified FROM users WHERE email = $1`
		queryParam = req.Email
	} else {
		query = `SELECT id, first_name, last_name, email, phone_number, phone_verified FROM users WHERE phone_number = $1`
		queryParam = req.PhoneNumber
	}

	err := db.QueryRow(ctx, query, queryParam).Scan(&user.ID, &user.FirstName, &user.LastName, &emailNull, &phoneNull, &isVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if emailNull.Valid {
		user.Email = emailNull.String
	}
	if phoneNull.Valid {
		user.PhoneNumber = phoneNull.String
	}

	if isVerified {
		if hasEmail {
			utils.WriteError(w, "email already verified", http.StatusBadRequest)
		} else {
			utils.WriteError(w, "phone number already verified", http.StatusBadRequest)
		}
		return
	}

	duration, err := strconv.Atoi(os.Getenv("OTP_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, "UPDATE users SET otp = $1, otp_expires = $2 WHERE id = $3", otp, expiryTime, user.ID)
	if err != nil {
		utils.Logger.Errorf("failed to update user otp: %v", err)
		utils.WriteError(w, "could not update otp", http.StatusInternalServerError)
		return
	}

	displayName := user.FirstName + " " + user.LastName
	var otpDestination string
	if hasEmail {
		otpDestination = "email"
		go func() {
			if err := utils.SendOTPEmail(user.Email, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send OTP email: %v", err)
			}
		}()
	} else {
		otpDestination = "phone number"
		go func() {
			if err := utils.SendOTPSMS(user.PhoneNumber, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send OTP SMS: %v", err)
			}
		}()
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("New OTP sent to your %s successfully", otpDestination),
	})
}

// ============================================================================
// POST /auth/login
// ============================================================================

// LoginUserHandler godoc
// @Summary      Login
// @Description  Authenticates a user with email or phone number and password. Returns a JWT token.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{email=string,phone_number=string,password=string}  true  "Login credentials — provide either email or phone_number"
// @Success      200   {object}  object{status=string,message=string,token=string,user=object{id=string,first_name=string,last_name=string,username=string,email=string,phone_number=string,status=string,active_role=string}}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Router       /auth/login [post]
func LoginUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type loginRequest struct {
		PhoneNumber string `json:"phone_number"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	hasPhone := req.PhoneNumber != ""
	hasEmail := req.Email != ""

	if hasPhone == hasEmail {
		utils.WriteError(w, "please provide either email or phone number", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		utils.WriteError(w, "password is required", http.StatusBadRequest)
		return
	}
	if hasPhone {
		if err := utils.ValidateNigerianPhone(req.PhoneNumber); err != nil {
			utils.WriteError(w, "enter a valid phone number", http.StatusBadRequest)
			return
		}
	}
	if hasEmail {
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	user := &authModels.User{}
	var emailNull, phoneNull sql.NullString
	var deletedAt *time.Time
	var query, queryParam string

	if hasEmail {
		query = `SELECT id, email_verified, phone_verified, auth_provider, first_name, last_name, username,
		                email, phone_number, password, status, active_role, deleted_at
		         FROM users WHERE email = $1`
		queryParam = req.Email
	} else {
		query = `SELECT id, email_verified, phone_verified, auth_provider, first_name, last_name, username,
		                email, phone_number, password, status, active_role, deleted_at
		         FROM users WHERE phone_number = $1`
		queryParam = req.PhoneNumber
	}

	err := db.QueryRow(ctx, query, queryParam).Scan(
		&user.ID, &user.EmailVerified, &user.PhoneVerified, &user.AuthProvider,
		&user.FirstName, &user.LastName, &user.Username, &emailNull, &phoneNull,
		&user.Password, &user.Status, &user.ActiveRole, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		utils.Logger.Errorf("database query error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if emailNull.Valid {
		user.Email = emailNull.String
	}
	if phoneNull.Valid {
		user.PhoneNumber = phoneNull.String
	}
	if deletedAt != nil {
		utils.WriteError(w, "this account has been deleted", http.StatusForbidden)
		return
	}
	if user.Status == "suspended" {
		utils.WriteError(w, "your account has been suspended, please contact support", http.StatusForbidden)
		return
	}
	if hasEmail && !user.EmailVerified {
		utils.WriteError(w, "your email is not verified", http.StatusForbidden)
		return
	}
	if hasPhone && !user.PhoneVerified {
		utils.WriteError(w, "your phone number is not verified", http.StatusForbidden)
		return
	}

	parts := strings.Split(user.Password, ".")
	if len(parts) != 2 {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}
	salt, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}
	storedHash, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		utils.WriteError(w, "invalid credentials", http.StatusForbidden)
		return
	}
	hash := argon2.IDKey([]byte(req.Password), salt, 1, 64*1024, 4, 32)
	if len(hash) != len(storedHash) || subtle.ConstantTimeCompare(hash, storedHash) != 1 {
		utils.WriteError(w, "incorrect password or credentials", http.StatusForbidden)
		return
	}

	displayName := user.FirstName + " " + user.LastName
	tokenString, err := utils.SignToken(user.ID, displayName, user.ActiveRole)
	if err != nil {
		utils.WriteError(w, "error signing in", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		utils.Logger.Errorf("failed to generate refresh token: %v", err)
		utils.WriteError(w, "verification successful but failed to create session", http.StatusInternalServerError)
		return
	}
	if err := utils.StoreRefreshToken(ctx, user.ID, refreshToken, ""); err != nil {
		utils.Logger.Errorf("failed to store refresh token: %v", err)
		utils.WriteError(w, "verification successful but failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(15 * time.Minute),
		SameSite: http.SameSiteNoneMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		SameSite: http.SameSiteStrictMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "login successful",
		"token":   tokenString,
		"user": map[string]interface{}{
			"id":           user.ID,
			"first_name":   user.FirstName,
			"last_name":    user.LastName,
			"username":     user.Username,
			"email":        user.Email,
			"phone_number": user.PhoneNumber,
			"status":       user.Status,
			"active_role":  user.ActiveRole,
		},
	})
}

// ============================================================================
// POST /auth/logout
// ============================================================================

// LogoutUserHandler godoc
// @Summary      Logout
// @Description  Clears the Bearer cookie and logs the user out.
// @Tags         Auth
// @Produce      json
// @Success      200  {object}  object{message=string}
// @Router       /auth/logout [post]
// @Security     BearerAuth
func LogoutUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteNoneMode,
	})
	utils.WriteJSON(w, map[string]string{"message": "logged out successfully"})

	if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
		hash := utils.HashRefreshToken(cookie.Value)
		go utils.RevokeRefreshToken(context.Background(), hash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteStrictMode,
	})
}

// ============================================================================
// PATCH /auth/password
// ============================================================================

// UpdateUserPasswordHandler godoc
// @Summary      Update password
// @Description  Updates the authenticated user's password. Requires the current password for verification.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  authModels.UpdatePasswordRequest  true  "Current and new password"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /auth/password [patch]
// @Security     BearerAuth
func UpdateUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(utils.ContextKey("userId")).(uuid.UUID)
	if !ok {
		utils.WriteError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var req authModels.UpdatePasswordRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "all fields are required", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.CurrentPassword == "" || req.NewPassword == "" {
		utils.WriteError(w, "please enter all fields", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var activeRole, firstName, lastName, userPassword string
	err := db.QueryRow(ctx,
		"SELECT password, first_name, last_name, active_role FROM users WHERE id = $1",
		userID,
	).Scan(&userPassword, &firstName, &lastName, &activeRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := utils.VerifyPassword(req.CurrentPassword, userPassword); err != nil {
		utils.WriteError(w, "the password you entered does not match the current password", http.StatusBadRequest)
		return
	}

	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx,
		"UPDATE users SET password = $1, password_changed_at = $2 WHERE id = $3",
		hashedPassword, time.Now(), userID,
	)
	if err != nil {
		utils.WriteError(w, "failed to update password", http.StatusInternalServerError)
		return
	}

	go utils.RevokeAllUserRefreshTokens(context.Background(), userID)

	displayName := firstName + " " + lastName
	token, err := utils.SignToken(userID, displayName, activeRole)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(24 * time.Hour),
		SameSite: http.SameSiteStrictMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "password updated successfully",
	})
}

// ============================================================================
// POST /auth/forgot-password
// ============================================================================

// ForgotUserPasswordHandler godoc
// @Summary      Forgot password
// @Description  Sends a password reset OTP to the user's email, phone number, or recovery email. Provide exactly one of the three fields.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{email=string,phone_number=string,recovery_email=string}  true  "Provide exactly one field"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Router       /auth/forgot-password [post]
func ForgotUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		PhoneNumber   string `json:"phone_number"`
		Email         string `json:"email"`
		RecoveryEmail string `json:"recovery_email"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.RecoveryEmail = strings.TrimSpace(strings.ToLower(req.RecoveryEmail))

	fieldCount := 0
	if req.PhoneNumber != "" {
		fieldCount++
	}
	if req.Email != "" {
		fieldCount++
	}
	if req.RecoveryEmail != "" {
		fieldCount++
	}
	if fieldCount != 1 {
		utils.WriteError(w, "please provide exactly one of: email, phone_number, or recovery_email", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber != "" {
		if err := utils.ValidateNigerianPhone(req.PhoneNumber); err != nil {
			utils.WriteError(w, "enter a valid phone number", http.StatusBadRequest)
			return
		}
	}
	if req.Email != "" {
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.RecoveryEmail != "" {
		if err := utils.ValidateEmail(req.RecoveryEmail); err != nil {
			utils.WriteError(w, "enter a valid recovery email address", http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var user authModels.User
	var primaryEmail, phoneNull, recoveryEmail sql.NullString
	var recoveryEmailVerified bool
	var query, queryParam string

	if req.Email != "" {
		query = `SELECT id, first_name, last_name, email, phone_number, recovery_email, recovery_email_verified
		         FROM users WHERE email = $1 AND deleted_at IS NULL`
		queryParam = req.Email
	} else if req.PhoneNumber != "" {
		query = `SELECT id, first_name, last_name, email, phone_number, recovery_email, recovery_email_verified
		         FROM users WHERE phone_number = $1 AND deleted_at IS NULL`
		queryParam = req.PhoneNumber
	} else {
		query = `SELECT id, first_name, last_name, email, phone_number, recovery_email, recovery_email_verified
		         FROM users WHERE recovery_email = $1 AND recovery_email_verified = TRUE AND deleted_at IS NULL`
		queryParam = req.RecoveryEmail
	}

	err := db.QueryRow(ctx, query, queryParam).Scan(
		&user.ID, &user.FirstName, &user.LastName, &primaryEmail, &phoneNull, &recoveryEmail, &recoveryEmailVerified,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteJSON(w, map[string]interface{}{
				"status":  "success",
				"message": "if an account exists, a reset OTP has been sent",
			})
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if primaryEmail.Valid {
		user.Email = primaryEmail.String
	}
	if phoneNull.Valid {
		user.PhoneNumber = phoneNull.String
	}

	duration, err := strconv.Atoi(os.Getenv("RESET_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.WriteError(w, "failed to generate otp", http.StatusInternalServerError)
		return
	}

	hashedOTP := sha256.Sum256([]byte(otp))
	hashedOTPString := hex.EncodeToString(hashedOTP[:])

	_, err = db.Exec(ctx,
		`UPDATE users SET password_reset_token = $1, password_token_expires = $2 WHERE id = $3`,
		hashedOTPString, expiryTime.Format(time.RFC3339), user.ID,
	)
	if err != nil {
		utils.Logger.Errorf("failed to store reset token: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := user.FirstName + " " + user.LastName
	var otpDestination string

	switch {
	case req.RecoveryEmail != "":
		otpDestination = "recovery email"
		go func() {
			if err := utils.SendPasswordResetEmail(req.RecoveryEmail, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send reset OTP to recovery email: %v", err)
			}
		}()
	case req.Email != "":
		otpDestination = "email"
		go func() {
			if err := utils.SendPasswordResetEmail(user.Email, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send reset OTP to email: %v", err)
			}
		}()
	case req.PhoneNumber != "":
		otpDestination = "phone number"
		go func() {
			if err := utils.SendOTPSMS(user.PhoneNumber, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send reset OTP SMS: %v", err)
			}
		}()
	}

	resp := map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("password reset OTP sent to your %s", otpDestination),
	}
	if req.PhoneNumber != "" && recoveryEmailVerified && recoveryEmail.Valid && recoveryEmail.String != "" {
		resp["hint"] = "you can also reset your password using your recovery email"
	}
	utils.WriteJSON(w, resp)
}

// ============================================================================
// PATCH /auth/forgot-password/reset
// ============================================================================

// ResetUserPasswordHandler godoc
// @Summary      Reset password
// @Description  Resets the user's password using the OTP sent via forgot-password.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  object{email=string,phone_number=string,otp=string,new_password=string,confirm_password=string}  true  "Reset payload"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Router       /auth/forgot-password/reset [patch]
func ResetUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	db := sqlconnect.DB
	if db == nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	type request struct {
		PhoneNumber     string `json:"phone_number"`
		Email           string `json:"email"`
		OTP             string `json:"otp"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	hasPhone := req.PhoneNumber != ""
	hasEmail := req.Email != ""

	if hasPhone == hasEmail {
		utils.WriteError(w, "please provide either email or phone number", http.StatusBadRequest)
		return
	}
	if req.OTP == "" {
		utils.WriteError(w, "OTP is required", http.StatusBadRequest)
		return
	}
	if req.NewPassword == "" || req.ConfirmPassword == "" {
		utils.WriteError(w, "new_password and confirm_password are required", http.StatusBadRequest)
		return
	}
	if req.NewPassword != req.ConfirmPassword {
		utils.WriteError(w, "passwords must match", http.StatusBadRequest)
		return
	}

	hashedOTP := sha256.Sum256([]byte(req.OTP))
	hashedOTPString := hex.EncodeToString(hashedOTP[:])

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var userID uuid.UUID
	var query, queryParam string

	if hasEmail {
		query = `SELECT id FROM users WHERE email = $1 AND password_reset_token = $2 AND password_token_expires > $3`
		queryParam = req.Email
	} else {
		query = `SELECT id FROM users WHERE phone_number = $1 AND password_reset_token = $2 AND password_token_expires > $3`
		queryParam = req.PhoneNumber
	}

	err := db.QueryRow(ctx, query, queryParam, hashedOTPString, time.Now().Format(time.RFC3339)).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired OTP", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, `
		UPDATE users SET password = $1, password_reset_token = NULL,
		    password_token_expires = NULL, password_changed_at = $2
		WHERE id = $3
	`, hashedPassword, time.Now(), userID)
	if err != nil {
		utils.WriteError(w, "could not update password", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "password reset successfully",
	})
}

// ============================================================================
// GET /auth/users/me
// ============================================================================

// GetUserMeHandler godoc
// @Summary      Get current user
// @Description  Returns the full profile of the authenticated user, including wallet info and all activated roles.
// @Tags         Users
// @Produce      json
// @Success      200  {object}  object{status=string,data=authModels.User,roles=[]string,wallet=object{id=string,user_id=string,balance=number,currency=string,is_active=bool,wallet_created_at=string,last_transaction_at=string}}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /auth/users/me [get]
// @Security     BearerAuth
func GetUserMeHandler(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query := `
		SELECT
			u.id, u.first_name, u.last_name, u.username, u.email, u.phone_number,
			u.email_verified, u.phone_verified, u.status,
			u.active_role, u.user_created_at, u.auth_provider,
			u.avatar, u.is_online, u.recovery_email, u.recovery_email_verified,
			w.id, w.user_id, w.balance, w.currency,
			w.wallet_created_at, w.last_transaction_at, w.is_active
		FROM users u
		LEFT JOIN wallets w ON u.id = w.user_id
		WHERE u.id = $1 AND u.deleted_at IS NULL
	`

	var user authModels.User
	var wallet walletModels.Wallet
	var phoneNull, emailNull, recoveryEmailNull sql.NullString
	var isOnline *bool
	var lastTransactionAt sql.NullTime
	var avatarJSON []byte
	var recoveryEmailVerified bool

	err := db.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.FirstName, &user.LastName, &user.Username, &emailNull, &phoneNull,
		&user.EmailVerified, &user.PhoneVerified, &user.Status,
		&user.ActiveRole, &user.UserCreatedAt, &user.AuthProvider,
		&avatarJSON, &isOnline, &recoveryEmailNull, &recoveryEmailVerified,
		&wallet.ID, &wallet.UserID, &wallet.Balance, &wallet.Currency,
		&wallet.WalletCreatedAt, &lastTransactionAt, &wallet.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("error fetching from database: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if emailNull.Valid {
		user.Email = emailNull.String
	}
	if phoneNull.Valid {
		user.PhoneNumber = phoneNull.String
	}
	if isOnline != nil {
		user.IsOnline = isOnline
	}
	if lastTransactionAt.Valid {
		wallet.LastTransactionAt = &lastTransactionAt.Time
	}
	if recoveryEmailNull.Valid {
		user.RecoveryEmail = &recoveryEmailNull.String
	}
	user.RecoveryEmailVerified = recoveryEmailVerified
	if len(avatarJSON) > 0 {
		var avatar authModels.UserAvatar
		if err := json.Unmarshal(avatarJSON, &avatar); err == nil {
			user.Avatar = &avatar
		}
	}

	roleRows, err := db.Query(ctx, `SELECT role FROM user_roles WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		utils.Logger.Errorf("failed to fetch user roles: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer roleRows.Close()

	var roles []string
	for roleRows.Next() {
		var role string
		if err := roleRows.Scan(&role); err != nil {
			continue
		}
		roles = append(roles, role)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data":   user,
		"roles":  roles,
		"wallet": wallet,
	})
}

// ============================================================================
// PATCH /auth/users/switch-role
// ============================================================================

// SwitchRoleHandler godoc
// @Summary      Switch active role
// @Description  Switches the user's active role. If switching to a role for the first time, that role is added to the user's role list. Issues a new JWT with the updated role.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{role=string}  true  "Target role: client, artisan, or owner"
// @Success      200   {object}  object{status=string,message=string,active_role=string,token=string}
// @Failure      400   {object}  object{error=string}
// @Failure      403   {object}  object{error=string}
// @Router       /auth/users/switch-role [patch]
// @Security     BearerAuth
func SwitchRoleHandler(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		Role string `json:"role"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.Role = strings.TrimSpace(strings.ToLower(req.Role))
	if !handlers.AllowedRoles[req.Role] {
		utils.WriteError(w, "invalid role: must be client, artisan, or owner", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var firstName, lastName, currentRole, status string
	var deletedAt *time.Time
	err := db.QueryRow(ctx,
		`SELECT first_name, last_name, active_role, status, deleted_at FROM users WHERE id = $1`,
		userID,
	).Scan(&firstName, &lastName, &currentRole, &status, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if deletedAt != nil {
		utils.WriteError(w, "account not found", http.StatusNotFound)
		return
	}
	if status == "suspended" {
		utils.WriteError(w, "your account is suspended", http.StatusForbidden)
		return
	}
	if currentRole == req.Role {
		utils.WriteError(w, fmt.Sprintf("you are already in %s mode", req.Role), http.StatusBadRequest)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role) VALUES ($1, $2)
		ON CONFLICT (user_id, role) DO NOTHING
	`, userID, req.Role)
	if err != nil {
		utils.Logger.Errorf("failed to insert user_role: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var isOnlineValue interface{}
	if req.Role == "artisan" {
		isOnlineValue = false
	}

	_, err = tx.Exec(ctx, `UPDATE users SET active_role = $1, is_online = $2 WHERE id = $3`,
		req.Role, isOnlineValue, userID)
	if err != nil {
		utils.Logger.Errorf("failed to switch role: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := firstName + " " + lastName
	token, err := utils.SignToken(userID, displayName, req.Role)
	if err != nil {
		utils.WriteError(w, "role switched but failed to reissue token", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "Bearer",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		SameSite: http.SameSiteNoneMode,
	})

	utils.WriteJSON(w, map[string]interface{}{
		"status":      "success",
		"message":     fmt.Sprintf("switched to %s mode", req.Role),
		"active_role": req.Role,
		"token":       token,
	})
}

// ============================================================================
// POST /devices/register
// ============================================================================

// RegisterDevice godoc
// @Summary      Register device for push notifications
// @Description  Registers a device FCM token for the authenticated user. device_type must be android or ios.
// @Tags         Devices
// @Accept       json
// @Produce      json
// @Param        body  body  object{fcm_token=string,device_type=string}  true  "Device registration payload"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /auth/devices/register [post]
// @Security     BearerAuth
func RegisterDevice(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		FCMToken   string `json:"fcm_token"`
		DeviceType string `json:"device_type"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.FCMToken = strings.TrimSpace(req.FCMToken)
	req.DeviceType = strings.ToLower(strings.TrimSpace(req.DeviceType))

	if req.FCMToken == "" {
		utils.WriteError(w, "fcm_token is required", http.StatusBadRequest)
		return
	}
	if req.DeviceType == "" {
		utils.WriteError(w, "device_type is required", http.StatusBadRequest)
		return
	}
	if !handlers.AllowedDeviceTypes[req.DeviceType] {
		utils.WriteError(w, "device_type must be android or ios", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(r.Context(), `
		INSERT INTO user_devices (user_id, fcm_token, device_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (fcm_token) DO UPDATE
			SET user_id = EXCLUDED.user_id, device_type = EXCLUDED.device_type, updated_at = now()
	`, userID, req.FCMToken, req.DeviceType)
	if err != nil {
		utils.Logger.Errorf("failed to register device: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]string{"status": "success", "message": "device registered successfully"})
}

// ============================================================================
// POST /auth/users/avatar
// ============================================================================

// UploadAvatar godoc
// @Summary      Upload avatar
// @Description  Uploads or replaces the authenticated user's profile picture. Accepts multipart/form-data with an avatar file field.
// @Tags         Users
// @Accept       mpfd
// @Produce      json
// @Param        avatar  formData  file  true  "Avatar image file"
// @Success      200     {object}  object{status=string,message=string,avatar=authModels.UserAvatar}
// @Failure      400     {object}  object{error=string}
// @Failure      401     {object}  object{error=string}
// @Router       /auth/users/avatar [post]
// @Security     BearerAuth
func UploadAvatar(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.WriteError(w, "failed to parse form data", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		utils.WriteError(w, "avatar file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var existingAvatarJSON []byte
	_ = db.QueryRow(ctx, `SELECT avatar FROM users WHERE id = $1`, userID).Scan(&existingAvatarJSON)

	cloud, err := utils.InitCloudinary()
	if err != nil {
		utils.WriteError(w, "failed to initialize cloudinary", http.StatusInternalServerError)
		return
	}

	if len(existingAvatarJSON) > 0 {
		var oldAvatar authModels.UserAvatar
		if json.Unmarshal(existingAvatarJSON, &oldAvatar) == nil && oldAvatar.PublicID != "" {
			handlers.CleanupUploads(ctx, cloud, []string{oldAvatar.PublicID})
		}
	}

	uploadFiles := []utils.UploadFile{{Reader: file, Filename: header.Filename}}
	urls, publicIDs, err := handlers.UploadFilesConcurrently(ctx, cloud, uploadFiles, "avatar")
	if err != nil || len(urls) == 0 {
		utils.WriteError(w, "failed to upload avatar", http.StatusInternalServerError)
		return
	}

	newAvatar := authModels.UserAvatar{URL: urls[0], PublicID: publicIDs[0]}
	avatarJSON, err := json.Marshal(newAvatar)
	if err != nil {
		handlers.CleanupUploads(ctx, cloud, publicIDs)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, `UPDATE users SET avatar = $1 WHERE id = $2`, avatarJSON, userID)
	if err != nil {
		handlers.CleanupUploads(ctx, cloud, publicIDs)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "avatar uploaded successfully",
		"avatar":  newAvatar,
	})
}

// ============================================================================
// POST /auth/users/verify/add-contact
// ============================================================================

// AddSecondaryContactHandler godoc
// @Summary      Add secondary contact
// @Description  Adds an email or phone number to an account that was created with only one contact.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{email=string,phone_number=string}  true  "Provide either email or phone_number"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/users/verify/add-contact [post]
// @Security     BearerAuth
func AddSecondaryContactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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

	type request struct {
		PhoneNumber string `json:"phone_number"`
		Email       string `json:"email"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	hasPhone := req.PhoneNumber != ""
	hasEmail := req.Email != ""

	if hasPhone == hasEmail {
		utils.WriteError(w, "please provide either email or phone number, not both", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var currentEmail, currentPhone sql.NullString
	var emailVerified, phoneVerified bool
	var firstName, lastName string

	err := db.QueryRow(ctx,
		`SELECT first_name, last_name, email, phone_number, email_verified, phone_verified FROM users WHERE id = $1`,
		userID,
	).Scan(&firstName, &lastName, &currentEmail, &currentPhone, &emailVerified, &phoneVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var updateField, otpDestination string

	if hasEmail {
		if currentEmail.Valid && currentEmail.String != "" {
			utils.WriteError(w, "you already have an email on your account", http.StatusBadRequest)
			return
		}
		if err := utils.ValidateEmail(req.Email); err != nil {
			utils.WriteError(w, err.Error(), http.StatusBadRequest)
			return
		}
		updateField = "email"
		otpDestination = "email"
	} else {
		if currentPhone.Valid && currentPhone.String != "" {
			utils.WriteError(w, "you already have a phone number on your account", http.StatusBadRequest)
			return
		}
		if err := utils.ValidateNigerianPhone(req.PhoneNumber); err != nil {
			utils.WriteError(w, "enter a valid phone number", http.StatusBadRequest)
			return
		}
		updateField = "phone_number"
		otpDestination = "phone number"
	}

	duration, err := strconv.Atoi(os.Getenv("OTP_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.WriteError(w, "failed to generate otp", http.StatusInternalServerError)
		return
	}

	contactValue := req.Email
	if !hasEmail {
		contactValue = req.PhoneNumber
	}

	_, err = db.Exec(ctx,
		fmt.Sprintf("UPDATE users SET %s = $1, otp = $2, otp_expires = $3 WHERE id = $4", updateField),
		contactValue, otp, expiryTime, userID,
	)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			utils.WriteError(w, fmt.Sprintf("that %s is already in use by another account", otpDestination), http.StatusConflict)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := firstName + " " + lastName
	if hasEmail {
		go func() {
			if err := utils.SendOTPEmail(req.Email, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send OTP email: %v", err)
			}
		}()
	} else {
		go func() {
			if err := utils.SendOTPSMS(req.PhoneNumber, displayName, otp, expiryTime); err != nil {
				utils.Logger.Errorf("failed to send OTP SMS: %v", err)
			}
		}()
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("OTP sent to your %s for verification", otpDestination),
	})
}

// ============================================================================
// POST /auth/users/verify/confirm-contact
// ============================================================================

// ConfirmSecondaryContactHandler godoc
// @Summary      Confirm secondary contact
// @Description  Verifies the OTP sent to the newly added email or phone number.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{otp=string}  true  "OTP code"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Router       /auth/users/verify/confirm-contact [post]
// @Security     BearerAuth
func ConfirmSecondaryContactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.WriteError(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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

	type request struct {
		Otp string `json:"otp"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Otp == "" {
		utils.WriteError(w, "please enter otp", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var currentEmail, currentPhone sql.NullString
	var emailVerified, phoneVerified bool

	err := db.QueryRow(ctx,
		`SELECT email, phone_number, email_verified, phone_verified
		 FROM users WHERE id = $1 AND otp = $2 AND otp_expires > $3`,
		userID, req.Otp, time.Now().Format(time.RFC3339),
	).Scan(&currentEmail, &currentPhone, &emailVerified, &phoneVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired otp", http.StatusBadRequest)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var updateQuery, verifiedField string
	if currentEmail.Valid && currentEmail.String != "" && !emailVerified {
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, email_verified = TRUE WHERE id = $1"
		verifiedField = "email"
	} else if currentPhone.Valid && currentPhone.String != "" && !phoneVerified {
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, phone_verified = TRUE WHERE id = $1"
		verifiedField = "phone number"
	} else {
		utils.WriteError(w, "no pending contact verification found", http.StatusBadRequest)
		return
	}

	_, err = db.Exec(ctx, updateQuery, userID)
	if err != nil {
		utils.WriteError(w, "could not verify otp", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("%s verified successfully", verifiedField),
	})
}

// ============================================================================
// POST /auth/users/recovery-email
// ============================================================================

// AddRecoveryEmailHandler godoc
// @Summary      Add recovery email
// @Description  Adds a recovery email to the user's account and sends an OTP to verify it.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{recovery_email=string}  true  "Recovery email address"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/users/recovery-email [post]
// @Security     BearerAuth
func AddRecoveryEmailHandler(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		RecoveryEmail string `json:"recovery_email"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	req.RecoveryEmail = strings.TrimSpace(strings.ToLower(req.RecoveryEmail))
	if req.RecoveryEmail == "" {
		utils.WriteError(w, "recovery_email is required", http.StatusBadRequest)
		return
	}
	if err := utils.ValidateEmail(req.RecoveryEmail); err != nil {
		utils.WriteError(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var primaryEmail sql.NullString
	var firstName, lastName string
	err := db.QueryRow(ctx, `SELECT first_name, last_name, email FROM users WHERE id = $1`, userID).
		Scan(&firstName, &lastName, &primaryEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if primaryEmail.Valid && strings.EqualFold(primaryEmail.String, req.RecoveryEmail) {
		utils.WriteError(w, "recovery email must be different from your primary email", http.StatusBadRequest)
		return
	}

	var conflict bool
	_ = db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1 AND id != $2)`,
		req.RecoveryEmail, userID,
	).Scan(&conflict)
	if conflict {
		utils.WriteError(w, "this email is already in use by another account", http.StatusConflict)
		return
	}

	duration, err := strconv.Atoi(os.Getenv("OTP_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.WriteError(w, "failed to generate otp", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx, `
		UPDATE users SET recovery_email = $1, recovery_email_verified = FALSE, otp = $2, otp_expires = $3
		WHERE id = $4
	`, req.RecoveryEmail, otp, expiryTime, userID)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			utils.WriteError(w, "this email is already in use", http.StatusConflict)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := firstName + " " + lastName
	go func() {
		if err := utils.SendOTPEmail(req.RecoveryEmail, displayName, otp, expiryTime); err != nil {
			utils.Logger.Errorf("failed to send recovery email OTP: %v", err)
		}
	}()

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "OTP sent to your recovery email — verify it to activate",
	})
}

// ============================================================================
// POST /auth/users/recovery-email/verify
// ============================================================================

// VerifyRecoveryEmailHandler godoc
// @Summary      Verify recovery email
// @Description  Confirms the OTP sent to the recovery email to activate it.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{otp=string}  true  "OTP code"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Router       /auth/users/recovery-email/verify [post]
// @Security     BearerAuth
func VerifyRecoveryEmailHandler(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		Otp string `json:"otp"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Otp == "" {
		utils.WriteError(w, "otp is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var recoveryEmail sql.NullString
	err := db.QueryRow(ctx,
		`SELECT recovery_email FROM users WHERE id = $1 AND otp = $2 AND otp_expires > $3`,
		userID, req.Otp, time.Now().Format(time.RFC3339),
	).Scan(&recoveryEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired otp", http.StatusBadRequest)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !recoveryEmail.Valid || recoveryEmail.String == "" {
		utils.WriteError(w, "no pending recovery email to verify", http.StatusBadRequest)
		return
	}

	_, err = db.Exec(ctx,
		`UPDATE users SET recovery_email_verified = TRUE, otp = NULL, otp_expires = NULL WHERE id = $1`,
		userID,
	)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "recovery email verified successfully",
	})
}

// ============================================================================
// DELETE /auth/users/recovery-email
// ============================================================================

// RemoveRecoveryEmailHandler godoc
// @Summary      Remove recovery email
// @Description  Removes the recovery email from the authenticated user's account.
// @Tags         Users
// @Produce      json
// @Success      200  {object}  object{status=string,message=string}
// @Failure      401  {object}  object{error=string}
// @Failure      404  {object}  object{error=string}
// @Router       /auth/users/recovery-email [delete]
// @Security     BearerAuth
func RemoveRecoveryEmailHandler(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := db.Exec(ctx,
		`UPDATE users SET recovery_email = NULL, recovery_email_verified = FALSE
		 WHERE id = $1 AND recovery_email IS NOT NULL`,
		userID,
	)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected() == 0 {
		utils.WriteError(w, "no recovery email found on your account", http.StatusNotFound)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "recovery email removed successfully",
	})
}

// ============================================================================
// GET /auth/users/contact-status
// ============================================================================

// GetContactStatusHandler godoc
// @Summary      Get contact status
// @Description  Returns whether the authenticated user has an email and/or phone number on their account.
// @Tags         Users
// @Produce      json
// @Success      200  {object}  object{status=string,data=object{has_email=bool,has_phone=bool,email_verified=bool,phone_verified=bool,is_complete=bool,missing=string}}
// @Failure      401  {object}  object{error=string}
// @Router       /auth/users/contact-status [get]
// @Security     BearerAuth
func GetContactStatusHandler(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var email, phone sql.NullString
	var emailVerified, phoneVerified bool

	err := db.QueryRow(ctx,
		`SELECT email, phone_number, email_verified, phone_verified FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &phone, &emailVerified, &phoneVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hasEmail := email.Valid && email.String != ""
	hasPhone := phone.Valid && phone.String != ""

	utils.WriteJSON(w, map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"has_email":      hasEmail,
			"has_phone":      hasPhone,
			"email_verified": emailVerified,
			"phone_verified": phoneVerified,
			"is_complete":    hasEmail && hasPhone,
			"missing":        handlers.MissingContact(hasEmail, hasPhone),
		},
	})
}

// ============================================================================
// DELETE /auth/users/account
// ============================================================================

// DeleteAccountHandler godoc
// @Summary      Delete account
// @Description  Soft-deletes the authenticated user's account. Requires password confirmation for local accounts.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{password=string}  true  "Password confirmation (required for local accounts)"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Failure      409   {object}  object{error=string}
// @Router       /auth/users/account [delete]
// @Security     BearerAuth
func DeleteAccountHandler(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		Password string `json:"password"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var activeRole, authProvider, hashedPassword string
	var alreadyDeleted *time.Time

	err := db.QueryRow(ctx,
		`SELECT active_role, auth_provider, COALESCE(password, ''), deleted_at FROM users WHERE id = $1`,
		userID,
	).Scan(&activeRole, &authProvider, &hashedPassword, &alreadyDeleted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if alreadyDeleted != nil {
		utils.WriteError(w, "account has already been deleted", http.StatusGone)
		return
	}

	if authProvider == "local" {
		if req.Password == "" {
			utils.WriteError(w, "password is required to delete your account", http.StatusBadRequest)
			return
		}
		if err := utils.VerifyPassword(req.Password, hashedPassword); err != nil {
			utils.WriteError(w, "incorrect password", http.StatusUnauthorized)
			return
		}
	}

	var activeBookingCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookings
		WHERE (client_id = $1 OR owner_id = $1)
		  AND status IN ('pending','confirmed','checked_in')
	`, userID).Scan(&activeBookingCount)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if activeBookingCount > 0 {
		utils.WriteError(w,
			fmt.Sprintf("you have %d active booking(s). Please complete or cancel them before deleting your account.", activeBookingCount),
			http.StatusConflict,
		)
		return
	}

	if activeRole == "artisan" {
		var activeJobCount int
		err = db.QueryRow(ctx, `
			SELECT COUNT(*) FROM jobs
			WHERE (client_id = $1 OR artisan_id = $1)
			  AND status IN ('pending','accepted','in_progress')
		`, userID).Scan(&activeJobCount)
		if err != nil {
			utils.WriteError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if activeJobCount > 0 {
			utils.WriteError(w,
				fmt.Sprintf("you have %d active job(s). Please complete or cancel them before deleting your account.", activeJobCount),
				http.StatusConflict,
			)
			return
		}
	}

	var openDisputeCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM disputes WHERE filed_by = $1 AND status IN ('open','investigating')
	`, userID).Scan(&openDisputeCount)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if openDisputeCount > 0 {
		utils.WriteError(w, "you have open dispute(s) that must be resolved before deleting your account.", http.StatusConflict)
		return
	}

	var walletBalance float64
	_ = db.QueryRow(ctx, `SELECT balance FROM wallets WHERE user_id = $1`, userID).Scan(&walletBalance)
	if walletBalance > 0 {
		utils.WriteError(w,
			fmt.Sprintf("you have ₦%.2f in your wallet. Please withdraw your funds before deleting your account.", walletBalance),
			http.StatusConflict,
		)
		return
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE users SET
			first_name           = 'Deleted',
			last_name            = 'User',
			username             = NULL,
			email                = NULL,
			phone_number         = NULL,
			avatar               = NULL,
			password             = NULL,
			password_reset_token = NULL,
			otp                  = NULL,
			is_online            = NULL,
			deleted_at           = $1
		WHERE id = $2
	`, time.Now(), userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `UPDATE wallets SET is_active = FALSE WHERE user_id = $1`, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `DELETE FROM user_devices WHERE user_id = $1`, userID)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.Logger.Infof("Account soft deleted: user_id=%s active_role=%s", userID, activeRole)

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "your account has been deleted successfully.",
	})
}

// ============================================================================
// GET /auth/users/check-username
// ============================================================================

// CheckUsernameHandler godoc
// @Summary      Check username availability
// @Description  Checks if a username is available. Uses a bloom filter — only hits the DB on a probable match.
// @Tags         Users
// @Produce      json
// @Param        username  query  string  true  "Username to check"
// @Success      200  {object}  object{available=bool,username=string}
// @Failure      400  {object}  object{error=string}
// @Router       /auth/users/check-username [get]
func CheckUsernameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("username")))
	if username == "" {
		utils.WriteError(w, "username is required", http.StatusBadRequest)
		return
	}
	if len(username) < 3 {
		utils.WriteError(w, "username must be at least 3 characters", http.StatusBadRequest)
		return
	}
	if len(username) > 50 {
		utils.WriteError(w, "username must be at most 50 characters", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	available, err := handlers.IsUsernameAvailable(ctx, username)
	if err != nil {
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"username":  username,
		"available": available,
	})
}

// ============================================================================
// POST /auth/users/verify/request-contact-otp
// ============================================================================

// RequestUnverifiedContactOTPHandler godoc
// @Summary      Request OTP for unverified contact
// @Description  Sends an OTP to the contact (email or phone) that was provided at signup but not yet verified.
//
//	Only works if the contact exists on the account and is still unverified.
//
// @Tags         Users
// @Produce      json
// @Success      200  {object}  object{status=string,message=string}
// @Failure      400  {object}  object{error=string}
// @Failure      401  {object}  object{error=string}
// @Router       /auth/users/verify/request-contact-otp [post]
// @Security     BearerAuth
func RequestUnverifiedContactOTPHandler(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var firstName, lastName string
	var emailNull, phoneNull sql.NullString
	var emailVerified, phoneVerified bool

	err := db.QueryRow(ctx,
		`SELECT first_name, last_name, email, phone_number, email_verified, phone_verified
		 FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&firstName, &lastName, &emailNull, &phoneNull, &emailVerified, &phoneVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "user not found", http.StatusNotFound)
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hasEmail := emailNull.Valid && emailNull.String != ""
	hasPhone := phoneNull.Valid && phoneNull.String != ""

	var sendToEmail, sendToPhone bool
	switch {
	case hasEmail && !emailVerified:
		sendToEmail = true
	case hasPhone && !phoneVerified:
		sendToPhone = true
	default:
		if hasEmail && hasPhone {
			utils.WriteError(w, "all contacts on your account are already verified", http.StatusBadRequest)
		} else {
			utils.WriteError(w, "no unverified contact found on your account", http.StatusBadRequest)
		}
		return
	}

	duration, err := strconv.Atoi(os.Getenv("OTP_TOKEN_EXP_DURATION"))
	if err != nil {
		utils.Logger.Error("failed to read OTP_TOKEN_EXP_DURATION")
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	expiryTime := time.Now().Add(time.Duration(duration) * time.Minute)
	otp, err := utils.GenerateSecureOTP()
	if err != nil {
		utils.Logger.Errorf("failed to generate otp: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(ctx,
		"UPDATE users SET otp = $1, otp_expires = $2 WHERE id = $3",
		otp, expiryTime, userID,
	)
	if err != nil {
		utils.Logger.Errorf("failed to update otp: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	displayName := firstName + " " + lastName
	var otpDestination string

	if sendToEmail {
		otpDestination = "email"
		go func(email, name, otp string, expiry time.Time) {
			if err := utils.SendOTPEmail(email, name, otp, expiry); err != nil {
				utils.Logger.Errorf("failed to send OTP email to %s: %v", email, err)
			}
		}(emailNull.String, displayName, otp, expiryTime)
	} else if sendToPhone {
		otpDestination = "phone number"
		go func(phone, name, otp string, expiry time.Time) {
			if err := utils.SendOTPSMS(phone, name, otp, expiry); err != nil {
				utils.Logger.Errorf("failed to send OTP SMS to %s: %v", phone, err)
			}
		}(phoneNull.String, displayName, otp, expiryTime)
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("OTP sent to your %s for verification", otpDestination),
	})
}

// ============================================================================
// POST /auth/users/verify/confirm-contact-otp
// ============================================================================

// ConfirmUnverifiedContactOTPHandler godoc
// @Summary      Confirm OTP for unverified contact
// @Description  Verifies the OTP sent to the previously unverified contact on the account.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        body  body  object{otp=string}  true  "OTP code"
// @Success      200   {object}  object{status=string,message=string}
// @Failure      400   {object}  object{error=string}
// @Failure      401   {object}  object{error=string}
// @Router       /auth/users/verify/confirm-contact-otp [post]
// @Security     BearerAuth
func ConfirmUnverifiedContactOTPHandler(w http.ResponseWriter, r *http.Request) {
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

	type request struct {
		Otp string `json:"otp"`
	}

	var req request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.WriteError(w, "invalid or unexpected fields in body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Otp == "" {
		utils.WriteError(w, "please enter otp", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var emailNull, phoneNull sql.NullString
	var emailVerified, phoneVerified bool

	err := db.QueryRow(ctx,
		`SELECT email, phone_number, email_verified, phone_verified
		 FROM users
		 WHERE id = $1 AND otp = $2 AND otp_expires > $3 AND deleted_at IS NULL`,
		userID, req.Otp, time.Now().Format(time.RFC3339),
	).Scan(&emailNull, &phoneNull, &emailVerified, &phoneVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, "invalid or expired otp", http.StatusBadRequest)
			return
		}
		utils.Logger.Errorf("database error: %v", err)
		utils.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var updateQuery, verifiedContact string
	switch {
	case emailNull.Valid && emailNull.String != "" && !emailVerified:
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, email_verified = TRUE WHERE id = $1"
		verifiedContact = "email"
	case phoneNull.Valid && phoneNull.String != "" && !phoneVerified:
		updateQuery = "UPDATE users SET otp = NULL, otp_expires = NULL, phone_verified = TRUE WHERE id = $1"
		verifiedContact = "phone number"
	default:
		utils.WriteError(w, "no unverified contact found to confirm", http.StatusBadRequest)
		return
	}

	if _, err = db.Exec(ctx, updateQuery, userID); err != nil {
		utils.Logger.Errorf("failed to mark contact verified: %v", err)
		utils.WriteError(w, "could not verify contact", http.StatusInternalServerError)
		return
	}

	utils.WriteJSON(w, map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("%s verified successfully", verifiedContact),
	})
}
