package utils

import (
	"encoding/json"
	"net/http"
)

func WriteJSONWithStatus(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "failed to encode JSON response", http.StatusInternalServerError)
	}
}
