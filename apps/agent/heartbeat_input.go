package main

import (
	"encoding/json"
	"strings"
)

// heartbeatMessageFromInput extracts heartbeat.message from a structured stdin line.
func heartbeatMessageFromInput(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return "", false
	}

	var hb struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &hb); err != nil {
		// Debug: log why the parse failed so we can diagnose encoding issues
		output.System("[DEBUG] heartbeatMessageFromInput: JSON parse failed: %v (input=%q)", err, truncate(trimmed, 120))
		return "", false
	}
	if hb.Type != "heartbeat" {
		return "", false
	}

	msg := strings.TrimSpace(hb.Message)
	if msg == "" {
		return "", false
	}
	return msg, true
}
