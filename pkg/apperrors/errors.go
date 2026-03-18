package apperrors

import (
	"errors"
	"net/http"
)

type AppError struct {
	Err        error
	Message    string
	StatusCode int
}

func (e *AppError) Error() string {
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

var (
	ErrDBNotInitialized      = errors.New("database not initialized")
	ErrPropertyNotFound      = errors.New("property not found")
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidRequest        = errors.New("invalid request")
	ErrNoFieldsToUpdate      = errors.New("no fields to update")
	ErrImageUploadLimit      = errors.New("total images cannot exceed 10")
	ErrImageUploadFailed     = errors.New("failed to upload image(s)")
	ErrEmptyOtp              = errors.New("please enter otp")
	ErrInvalidOrExpiredOtp   = errors.New("invalid or expired otp")
	ErrCouldNotVerifyOtp     = errors.New("error verifying otp")
	ErrAgentNotFound         = errors.New("agent not found")
	ErrAlreadySubscribed     = errors.New("you are already subscribed to this plan")
	ErrSubscriptionNotFound  = errors.New("subscription plan not found")
	ErrVideoUploadLimit      = errors.New("you can upload a maximum of 3 videos per property")
	ErrVideoUploadFailed     = errors.New("failed to upload videos")
	ErrInvalidOrExpiredToken = errors.New("invalid or expired token")
)

func NewInvalidOrExpiredTokenError() error {
	return &AppError{
		Err:        ErrInvalidOrExpiredToken,
		Message:    "invalid or expired token",
		StatusCode: http.StatusBadRequest,
	}
}

func IsInvalidOrExpiredTokenError(err error) bool {
	return errors.Is(err, ErrInvalidOrExpiredToken)
}

func NewVideoUploadLimitError() error {
	return &AppError{
		Err:        ErrVideoUploadLimit,
		Message:    "you can upload a maximum of 3 videos per property",
		StatusCode: http.StatusBadRequest,
	}
}

func NewVideoUploadFailedError() error {
	return &AppError{
		Err:        ErrVideoUploadFailed,
		Message:    "failed to upload videos",
		StatusCode: http.StatusInternalServerError,
	}
}

func NewDBNotInitializedError() *AppError {
	return &AppError{
		Err:        ErrDBNotInitialized,
		Message:    "internal server error",
		StatusCode: http.StatusInternalServerError,
	}
}

func NewPropertyNotFoundError() *AppError {
	return &AppError{
		Err:        ErrPropertyNotFound,
		Message:    "property not found",
		StatusCode: http.StatusNotFound,
	}
}

func NewForbiddenError(message string) *AppError {
	return &AppError{
		Err:        ErrForbidden,
		Message:    message,
		StatusCode: http.StatusForbidden,
	}
}

func NewInvalidRequestError(message string) *AppError {
	return &AppError{
		Err:        ErrInvalidRequest,
		Message:    message,
		StatusCode: http.StatusBadRequest,
	}
}

func NewNoFieldsToUpdateError() *AppError {
	return &AppError{
		Err:        ErrNoFieldsToUpdate,
		Message:    "no fields to update",
		StatusCode: http.StatusBadRequest,
	}
}

func NewImageUploadLimitError() *AppError {
	return &AppError{
		Err:        ErrImageUploadLimit,
		Message:    "total images cannot exceed 10",
		StatusCode: http.StatusBadRequest,
	}
}

func NewImageUploadFailedError() *AppError {
	return &AppError{
		Err:        ErrImageUploadFailed,
		Message:    "failed to upload image(s)",
		StatusCode: http.StatusBadRequest,
	}
}

func NewInternalError(err error) *AppError {
	return &AppError{
		Err:        err,
		Message:    "internal server error",
		StatusCode: http.StatusInternalServerError,
	}
}

func NewEmptyOtpError(message string) *AppError {
	return &AppError{
		Err:        ErrEmptyOtp,
		Message:    message,
		StatusCode: http.StatusBadRequest,
	}
}

func NewInvalidOrExpiredOtpError() *AppError {
	return &AppError{
		Err:        ErrInvalidOrExpiredOtp,
		Message:    "invalid or expired otp",
		StatusCode: http.StatusBadRequest,
	}
}

func NewCouldNotVerifyOtpError() *AppError {
	return &AppError{
		Err:        ErrCouldNotVerifyOtp,
		Message:    "error verifying otp",
		StatusCode: http.StatusBadRequest,
	}
}

func NewAgentNotFoundError() *AppError {
	return &AppError{
		Err:        ErrAgentNotFound,
		Message:    "agent not found",
		StatusCode: http.StatusNotFound,
	}
}

func NewSubscriptionPlanNotFoundError() *AppError {
	return &AppError{
		Err:        ErrSubscriptionNotFound,
		Message:    "subscription plan not found",
		StatusCode: http.StatusNotFound,
	}
}

func NewAlreadySubscribedError(message string) *AppError {
	return &AppError{
		Err:        ErrAlreadySubscribed,
		Message:    message,
		StatusCode: http.StatusBadRequest,
	}
}

func NewFetchingPlanError(err error) *AppError {
	return &AppError{
		Err:        err,
		Message:    "error fetching plans",
		StatusCode: http.StatusInternalServerError,
	}
}
