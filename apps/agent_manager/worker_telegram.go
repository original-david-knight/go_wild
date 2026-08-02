package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/tools"
)

// TelegramWorker monitors Telegram messages for an agent.
type TelegramWorker struct {
	agentID string
	tools   *tools.TelegramTools
	manager *WorkerManager
	cancel  context.CancelFunc

	mu        sync.Mutex
	lastHBeat time.Time // rate limiting: min 30s between heartbeats
}

// NewTelegramWorker creates a new TelegramWorker.
func NewTelegramWorker(agentID string, manager *WorkerManager) *TelegramWorker {
	return &TelegramWorker{
		agentID: agentID,
		manager: manager,
	}
}

func (tw *TelegramWorker) Name() string {
	return "telegram"
}

// Start initializes the Telegram poller and sets up the callback.
func (tw *TelegramWorker) Start(ctx context.Context) error {
	t, err := tw.manager.telegram.getOrCreateTelegram(ctx, tw.agentID)
	if err != nil {
		return fmt.Errorf("failed to create telegram poller: %w", err)
	}
	tw.tools = t

	ctx, cancel := context.WithCancel(ctx)
	tw.mu.Lock()
	tw.cancel = cancel
	tw.mu.Unlock()

	// Set up callback for new messages
	tw.tools.SetOnNewMessages(func(messages []tools.TelegramMessage) {
		tw.handleNewMessages(messages)
	})

	log.Printf("TelegramWorker started for agent %s (@%s)", tw.agentID, tw.tools.GetBotUsername())

	// Block until context is cancelled
	go func() {
		<-ctx.Done()
		log.Printf("TelegramWorker stopped for agent %s", tw.agentID)
	}()

	return nil
}

// Stop cancels the worker context.
func (tw *TelegramWorker) Stop() {
	tw.mu.Lock()
	telegramTools := tw.tools
	cancel := tw.cancel
	tw.mu.Unlock()

	// Clear callback synchronously before cancel to avoid stale worker races.
	if telegramTools != nil {
		telegramTools.SetOnNewMessages(nil)
	}
	if cancel != nil {
		cancel()
	}
}

// handleNewMessages stores messages in the database and sends a heartbeat.
func (tw *TelegramWorker) handleNewMessages(messages []tools.TelegramMessage) {
	ctx := context.Background()
	dao := tw.manager.db.ForUser(tw.agentID).Table(data.TelegramMessageRecord{})

	stored := 0
	for _, msg := range messages {
		// Check for duplicate by update_id
		existing, err := dao.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"update_id": msg.UpdateID},
			Limit: 1,
		})
		if err == nil && len(existing) > 0 {
			continue // Already stored
		}

		record := data.TelegramMessageRecord{
			ID:        uuid.New().String(),
			AgentID:   tw.agentID,
			UpdateID:  msg.UpdateID,
			MessageID: msg.MessageID,
			ChatID:    msg.ChatID,
			ChatType:  msg.ChatType,
			ChatTitle: msg.ChatTitle,
			FromID:    msg.FromID,
			FromName:  msg.FromName,
			Username:  msg.Username,
			Text:      msg.Text,
			Date:      msg.Date,
			ReplyToID: msg.ReplyToID,
			CreatedAt: time.Now().UTC(),
		}

		if err := dao.Insert(ctx, record); err != nil {
			log.Printf("Failed to store telegram message for agent %s: %v", tw.agentID, err)
			continue
		}
		stored++
	}

	if stored == 0 {
		return
	}

	log.Printf("Stored %d telegram message(s) for agent %s", stored, tw.agentID)

	// Rate-limit heartbeats: min 30s between sends
	tw.mu.Lock()
	elapsed := time.Since(tw.lastHBeat)
	if elapsed < 30*time.Second {
		tw.mu.Unlock()
		return
	}
	tw.lastHBeat = time.Now()
	tw.mu.Unlock()

	// Send heartbeat to agent
	msg := "Check telegram and respond to all messages. Do not do any research or other work on this heartbeat unless needed to answer the message. Answer messages and then finish. no need to save info or context if there are no messages. don't sleep this command, just close it"
	if err := tw.manager.SendHeartbeat(tw.agentID, msg); err != nil {
		log.Printf("Failed to send heartbeat to agent %s: %v", tw.agentID, err)
	} else {
		// Save to chat history so it's visible in the terminal
		tw.manager.service.SaveChatMessage(ctx, tw.agentID, "user", msg)
		log.Printf("Sent telegram heartbeat to agent %s (%d new messages)", tw.agentID, stored)
	}
}
