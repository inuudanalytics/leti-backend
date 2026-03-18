package utils

import (
	"errors"
	"leti_server/pkg/apperrors"
	"net/http"
)

func HandleError(w http.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		WriteError(w, appErr.Message, appErr.StatusCode)
		return
	}

	Logger.Errorf("unexpected error: %v", err)
	WriteError(w, "internal server error", http.StatusInternalServerError)
}
