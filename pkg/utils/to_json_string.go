package utils

import (
	"encoding/json"
	"log"
)

// ToJSONString converts Go data structures to JSON strings safely
func ToJSONString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("JSON marshal error: %v\n", err)
		return "[]"
	}
	return string(data)
}
