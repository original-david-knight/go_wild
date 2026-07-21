package main

import (
	"sync"

	"github.com/original-david-knight/go_wild/tools"
)

// BrokerTelegramHandler handles telegram proxy requests.
type BrokerTelegramHandler struct {
	service       *AgentService
	workerManager *WorkerManager // set after construction

	mu      sync.Mutex
	pollers map[string]*tools.TelegramTools // agentID -> active telegram poller (fallback)
}

// NewBrokerTelegramHandler creates a new telegram broker handler.
func NewBrokerTelegramHandler(service *AgentService) *BrokerTelegramHandler {
	return &BrokerTelegramHandler{
		service: service,
		pollers: make(map[string]*tools.TelegramTools),
	}
}
