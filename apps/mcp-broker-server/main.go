package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	brokerURL := strings.TrimRight(os.Getenv("BROKER_URL"), "/")
	if brokerURL == "" {
		brokerURL = "http://localhost:8888"
	}

	agentID := os.Getenv("AGENT_ID")
	if agentID == "" {
		log.Fatal("AGENT_ID environment variable is required")
	}

	brokerSecret := os.Getenv("BROKER_SECRET")
	if brokerSecret == "" {
		log.Fatal("BROKER_SECRET environment variable is required")
	}

	secret, err := base64.StdEncoding.DecodeString(brokerSecret)
	if err != nil {
		log.Fatalf("Invalid BROKER_SECRET (must be base64): %v", err)
	}

	token := generateBrokerToken(secret, agentID)
	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))

	var disabledTools map[string]struct{}
	if raw := strings.TrimSpace(os.Getenv("DISABLED_TOOLS")); raw != "" {
		disabledTools = make(map[string]struct{})
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				disabledTools[t] = struct{}{}
			}
		}
	}

	server := &Server{
		brokerURL:       brokerURL,
		token:           token,
		executionMethod: executionMethod,
		disabledTools:   disabledTools,
	}

	if err := server.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// generateBrokerToken creates a token: base64url(agentID).base64url(HMAC-SHA256(secret, agentID))
func generateBrokerToken(secret []byte, agentID string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(agentID))
	sig := mac.Sum(nil)

	encodedID := base64.RawURLEncoding.EncodeToString([]byte(agentID))
	encodedSig := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s", encodedID, encodedSig)
}
