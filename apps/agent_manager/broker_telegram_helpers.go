package main

import (
	"context"
	"fmt"
	"log"

	"github.com/original-david-knight/go_wild/tools"
)

// getOrCreateTelegram returns existing or creates new TelegramTools for the agent.
// Checks the worker manager first (if available), then falls back to local pollers.
func (h *BrokerTelegramHandler) getOrCreateTelegram(ctx context.Context, agentID string) (*tools.TelegramTools, error) {
	// Check worker manager first
	if h.workerManager != nil {
		if t := h.workerManager.GetTelegramTools(agentID); t != nil {
			return t, nil
		}
	}

	h.mu.Lock()
	if t, ok := h.pollers[agentID]; ok {
		h.mu.Unlock()
		return t, nil
	}
	h.mu.Unlock()

	// Load token from database
	agent, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.TelegramBotToken == "" {
		return nil, fmt.Errorf("agent has no Telegram bot token configured")
	}

	t := tools.NewTelegramTools(agent.TelegramBotToken)
	// Use a background context for the poller — it must outlive the HTTP request
	// that triggers its creation. The request context is only used for getMe() validation.
	if err := t.Start(context.Background()); err != nil {
		return nil, err
	}

	h.mu.Lock()
	// Double-check after lock
	if existing, ok := h.pollers[agentID]; ok {
		h.mu.Unlock()
		t.Stop()
		return existing, nil
	}
	h.pollers[agentID] = t
	h.mu.Unlock()

	log.Printf("Started Telegram poller for agent %s (@%s)", agentID, t.GetBotUsername())
	return t, nil
}

// StopAll stops all active telegram pollers.
func (h *BrokerTelegramHandler) StopAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.pollers {
		t.Stop()
		log.Printf("Stopped Telegram poller for agent %s", id)
	}
	h.pollers = make(map[string]*tools.TelegramTools)
}
