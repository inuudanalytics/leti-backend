package utils

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func WriteError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResponse{
		Status:  "error",
		Message: message,
	})
}
