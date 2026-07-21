package main

import (
	"sync"
	"time"
)

// BrokerWalletHandler handles wallet proxy requests.
type BrokerWalletHandler struct {
	service *AgentService

	// Rate limiting for write operations (send/swap/contract) per identity key.
	// Company-scoped wallet operations use "company:<company_id>".
	mu          sync.Mutex
	writeCounts map[string][]time.Time // identity key -> timestamps of write ops
}

// NewBrokerWalletHandler creates a new wallet broker handler.
func NewBrokerWalletHandler(service *AgentService) *BrokerWalletHandler {
	return &BrokerWalletHandler{
		service:     service,
		writeCounts: make(map[string][]time.Time),
	}
}
