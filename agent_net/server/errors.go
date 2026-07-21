package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// ErrorCode constants for API errors.
const (
	ErrCodeInvalidTimestamp    = "INVALID_TIMESTAMP"
	ErrCodeInvalidSignature    = "INVALID_SIGNATURE"
	ErrCodeMissingPoWOrPremium = "MISSING_POW_OR_PREMIUM"
	ErrCodeKeyRevoked          = "KEY_REVOKED"
	ErrCodeRateLimited         = "RATE_LIMITED"
	ErrCodeBadRequest          = "BAD_REQUEST"
	ErrCodeNotFound            = "NOT_FOUND"
	ErrCodeInternalError       = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
	ErrCodeInvalidPoW          = "INVALID_POW"
	ErrCodeReplayDetected      = "REPLAY_DETECTED"
	ErrCodeUpgradeFailed       = "UPGRADE_FAILED"

	// Messaging error codes.
	ErrCodePremiumRequired     = "PREMIUM_REQUIRED"
	ErrCodeRecipientNotPremium = "RECIPIENT_NOT_PREMIUM"
	ErrCodeSelfMessage         = "SELF_MESSAGE"
	ErrCodeMessageNotFound     = "MESSAGE_NOT_FOUND"
	ErrCodeNotRecipient        = "NOT_RECIPIENT"
	ErrCodeNotSender           = "NOT_SENDER"
	ErrCodeMessageTooLarge     = "MESSAGE_TOO_LARGE"

	// A2A error codes.
	ErrCodeA2AJobNotFound   = "A2A_JOB_NOT_FOUND"
	ErrCodeA2AForbidden     = "A2A_FORBIDDEN"
	ErrCodeA2AInvalidState  = "A2A_INVALID_STATE"
	ErrCodeA2ALeaseExpired  = "A2A_LEASE_EXPIRED"
	ErrCodeA2APayloadTooBig = "A2A_PAYLOAD_TOO_LARGE"
)

// ErrorResponse represents a structured API error.
type ErrorResponse struct {
	Error              string       `json:"error"`
	Message            string       `json:"message"`
	ServerTime         string       `json:"server_time,omitempty"`
	UpgradeInfo        *UpgradeInfo `json:"upgrade_info,omitempty"`
	RequiredDifficulty int          `json:"required_pow_difficulty,omitempty"`
	RetryAfterSeconds  int          `json:"retry_after_seconds,omitempty"`
	Limit              string       `json:"limit,omitempty"`
	UpgradeHint        string       `json:"upgrade_hint,omitempty"`
}

// UpgradeInfo provides upgrade instructions.
type UpgradeInfo struct {
	TreasuryEndpoint string            `json:"treasury_endpoint"`
	RequiredAmounts  map[string]string `json:"required_amounts"`
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, statusCode int, errResp ErrorResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errResp)
}

// writeBadRequest writes a 400 error.
func writeBadRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, ErrorResponse{
		Error:   ErrCodeBadRequest,
		Message: message,
	})
}

// writeTimestampError writes a 400 timestamp error with helpful diagnostics.
func writeTimestampError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, ErrorResponse{
		Error:      ErrCodeInvalidTimestamp,
		Message:    message + " (hint: timestamp must be UTC, not local time)",
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	})
}

// writeUnauthorized writes a 401 error.
func writeUnauthorized(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnauthorized, ErrorResponse{
		Error:   ErrCodeInvalidSignature,
		Message: message,
	})
}

// SignatureErrorResponse provides detailed signature failure diagnostics.
type SignatureErrorResponse struct {
	Error          string `json:"error"`
	Message        string `json:"message"`
	SignatureInput string `json:"signature_input_format"`
	ProvidedValues struct {
		Method    string `json:"method"`
		Path      string `json:"path"`
		Timestamp string `json:"timestamp"`
	} `json:"provided_values"`
	CommonCauses []string `json:"common_causes"`
}

// writeSignatureError writes a detailed 401 signature error with debugging hints.
func writeSignatureError(w http.ResponseWriter, method, path, timestamp string) {
	resp := SignatureErrorResponse{
		Error:          ErrCodeInvalidSignature,
		Message:        "Signature verification failed",
		SignatureInput: "method:path:timestamp:SHA256(body)",
	}
	resp.ProvidedValues.Method = method
	resp.ProvidedValues.Path = path
	resp.ProvidedValues.Timestamp = timestamp
	resp.CommonCauses = []string{
		"Timestamp not in UTC (use time.Now().UTC(), not time.Now())",
		"Body was modified after signing (whitespace, key ordering)",
		"Wrong private key used for signing",
		"Timestamp in header doesn't match timestamp used in signature",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(resp)
}

// writePaymentRequired writes a 402 error for missing PoW/premium.
func writePaymentRequired(w http.ResponseWriter, message string, difficulty int) {
	writeError(w, http.StatusPaymentRequired, ErrorResponse{
		Error:   ErrCodeMissingPoWOrPremium,
		Message: message,
		UpgradeInfo: &UpgradeInfo{
			TreasuryEndpoint: "/api/v1/treasury",
			RequiredAmounts: map[string]string{
				"solana":   "0.005 SOL",
				"ethereum": "0.005 ETH",
			},
		},
		RequiredDifficulty: difficulty,
	})
}

// writeInvalidPoW writes a 402 error for invalid PoW.
func writeInvalidPoW(w http.ResponseWriter, message string, difficulty int) {
	writeError(w, http.StatusPaymentRequired, ErrorResponse{
		Error:              ErrCodeInvalidPoW,
		Message:            message,
		RequiredDifficulty: difficulty,
	})
}

// writeForbidden writes a 403 error.
func writeForbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, ErrorResponse{
		Error:   ErrCodeKeyRevoked,
		Message: message,
	})
}

// writeNotFound writes a 404 error.
func writeNotFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, ErrorResponse{
		Error:   ErrCodeNotFound,
		Message: message,
	})
}

// writeRateLimited writes a 429 error.
func writeRateLimited(w http.ResponseWriter, message string, retryAfter int, limit string) {
	w.Header().Set("Retry-After", string(rune(retryAfter)))
	writeError(w, http.StatusTooManyRequests, ErrorResponse{
		Error:             ErrCodeRateLimited,
		Message:           message,
		RetryAfterSeconds: retryAfter,
		Limit:             limit,
		UpgradeHint:       "Premium accounts have higher limits.",
	})
}

// writeInternalError writes a 500 error.
func writeInternalError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusInternalServerError, ErrorResponse{
		Error:   ErrCodeInternalError,
		Message: message,
	})
}

// writeReplayDetected writes a 400 error for replay attacks.
func writeReplayDetected(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, ErrorResponse{
		Error:   ErrCodeReplayDetected,
		Message: message,
	})
}

// writeUpgradeFailed writes an upgrade failure error.
func writeUpgradeFailed(w http.ResponseWriter, statusCode int, message string) {
	writeError(w, statusCode, ErrorResponse{
		Error:   ErrCodeUpgradeFailed,
		Message: message,
	})
}

// writePremiumRequired writes a 402 error for premium-only features.
func writePremiumRequired(w http.ResponseWriter, message string) {
	writeError(w, http.StatusPaymentRequired, ErrorResponse{
		Error:   ErrCodePremiumRequired,
		Message: message,
		UpgradeInfo: &UpgradeInfo{
			TreasuryEndpoint: "/api/v1/treasury",
			RequiredAmounts: map[string]string{
				"solana":   "0.005 SOL",
				"ethereum": "0.005 ETH",
			},
		},
	})
}

// writeMessageError writes a messaging-specific error with the given code.
func writeMessageError(w http.ResponseWriter, statusCode int, code, message string) {
	writeError(w, statusCode, ErrorResponse{
		Error:   code,
		Message: message,
	})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
