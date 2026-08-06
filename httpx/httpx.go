// Package httpx holds the small HTTP conventions a JSON API shares: the JSON
// writer, the `{"error": "..."}` envelope, and the server-sent-events frame
// writer.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON writes v as JSON with the given status. API responses are never
// cached: an answer reflects current state, not a cached one.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

// WriteError writes the shared API error shape.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// DecodeJSON reads a JSON request body, answering 400 with the shared error
// shape on malformed input. It reports whether decoding succeeded.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, hint string) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		WriteError(w, http.StatusBadRequest, hint)
		return false
	}
	return true
}
