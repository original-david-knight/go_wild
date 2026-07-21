package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/original-david-knight/go_wild/tools"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// Worker is the interface for background workers.
type Worker interface {
	Name() string
	Start(ctx context.Context) error
	Stop()
}

// TelegramProvider abstracts telegram poller creation so that WorkerManager
// does not depend on the concrete BrokerTelegramHandler type. This breaks
// the mutual dependency between workers and broker_telegram, enabling future
// extraction into separate sub-packages.
type TelegramProvider interface {
	getOrCreateTelegram(ctx context.Context, agentID string) (*tools.TelegramTools, error)
}

// WorkerManager manages background workers for agents.
type WorkerManager struct {
	mu       sync.RWMutex
	workers  map[string][]Worker // agentID -> workers
	hub      *SessionHub
	service  *AgentService
	telegram TelegramProvider
	db       gowild_data.Database
}

// NewWorkerManager creates a new WorkerManager.
func NewWorkerManager(hub *SessionHub, service *AgentService, telegram TelegramProvider, db gowild_data.Database) *WorkerManager {
	return &WorkerManager{
		workers:  make(map[string][]Worker),
		hub:      hub,
		service:  service,
		telegram: telegram,
		db:       db,
	}
}

// StartAgent checks agent config and creates/starts appropriate workers.
func (wm *WorkerManager) StartAgent(agentID string) error {
	agent, err := wm.service.GetAgent(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent %s: %w", agentID, err)
	}

	var workers []Worker

	// Create TelegramWorker if bot token is configured
	if agent.TelegramBotToken != "" {
		tw := NewTelegramWorker(agentID, wm)
		workers = append(workers, tw)
	}

	if len(workers) == 0 {
		return nil
	}

	wm.mu.Lock()
	// Stop existing workers first
	if existing, ok := wm.workers[agentID]; ok {
		for _, w := range existing {
			w.Stop()
		}
	}
	wm.workers[agentID] = workers
	wm.mu.Unlock()

	// Start all workers
	for _, w := range workers {
		log.Printf("Starting worker %s for agent %s", w.Name(), agentID)
		if err := w.Start(context.Background()); err != nil {
			log.Printf("Failed to start worker %s for agent %s: %v", w.Name(), agentID, err)
		}
	}

	return nil
}

// StopAgent stops all workers for an agent.
func (wm *WorkerManager) StopAgent(agentID string) {
	wm.mu.Lock()
	workers, ok := wm.workers[agentID]
	if ok {
		delete(wm.workers, agentID)
	}
	wm.mu.Unlock()

	if ok {
		for _, w := range workers {
			log.Printf("Stopping worker %s for agent %s", w.Name(), agentID)
			w.Stop()
		}
	}
}

// StopAll stops all workers (for shutdown).
func (wm *WorkerManager) StopAll() {
	wm.mu.Lock()
	allWorkers := make(map[string][]Worker)
	for k, v := range wm.workers {
		allWorkers[k] = v
	}
	wm.workers = make(map[string][]Worker)
	wm.mu.Unlock()

	for agentID, workers := range allWorkers {
		for _, w := range workers {
			log.Printf("Stopping worker %s for agent %s (shutdown)", w.Name(), agentID)
			w.Stop()
		}
	}
}

// SendHeartbeat writes a heartbeat JSON message to the agent's stdin via the session hub.
func (wm *WorkerManager) SendHeartbeat(agentID, message string) error {
	session, err := wm.hub.GetOrCreateSession(agentID)
	if err != nil {
		return fmt.Errorf("failed to get session for %s: %w", agentID, err)
	}

	hb := struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{
		Type:    "heartbeat",
		Message: message,
	}

	data, err := json.Marshal(hb)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	return session.writeToAgent(string(data) + "\n")
}

// GetTelegramTools returns the TelegramTools instance for a running worker, or nil.
func (wm *WorkerManager) GetTelegramTools(agentID string) *tools.TelegramTools {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	workers, ok := wm.workers[agentID]
	if !ok {
		return nil
	}

	for _, w := range workers {
		if tw, ok := w.(*TelegramWorker); ok {
			return tw.tools
		}
	}
	return nil
}
