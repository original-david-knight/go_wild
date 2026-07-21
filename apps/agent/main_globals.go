package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
	"github.com/original-david-knight/go_wild/tools/broker"
)

var (
	// Global readline instance for cleanup on exit
	globalReadline *readline.Instance

	// Tool call tracking
	toolCallCounts = make(map[string]int)

	// Work tasks mode - when enabled, agent continues working through task list
	workTasksMode      = false
	workTasksStartTime time.Time

	// Global Telegram tools (for heartbeat message notifications)
	globalTelegramTools *tools.TelegramTools

	// Global Email tools (for inbox info display)
	globalEmailTools *tools.EmailTools

	// Global Email outbox (for approval queue)
	globalEmailOutbox *tools.EmailOutbox

	// Global session logger (for -log flag)
	globalSessionLogger *SessionLogger

	// Global broker client (for heartbeat task queries in broker mode)
	globalBrokerClient *broker.Client

	// Global direct agent service (for local DB-backed tools in direct mode)
	globalAgentService *data.AgentService

	// Global resolved LLM session config for direct re-initialization paths.
	globalLLMConfig llmSessionConfig

	// Tracks whether A2A broker tools were registered for this session.
	globalA2AToolsEnabled bool

	// Global MCP clients (for cleanup on exit)
	globalMCPClients   []io.Closer
	globalMCPClientsMu sync.Mutex
)

// Smart mode configuration
const (
	smartThinkingBudget  = int32(24000) // High thinking budget for complex reasoning
	normalThinkingBudget = int32(0)     // No extended thinking in normal mode
)

// modelPair holds the base and smart model names from agent config.
type modelPair struct {
	base  string
	smart string
}

// llmSessionConfig holds the resolved provider/auth/model settings for this run.
type llmSessionConfig struct {
	Provider       string
	OpenAIAuthMode string
	BaseModel      string
	SmartModel     string
}

func (c llmSessionConfig) initialModel(smart bool) (string, error) {
	if smart {
		if c.SmartModel == "" {
			return "", fmt.Errorf("smart mode requested but smart model is not configured")
		}
		return c.SmartModel, nil
	}
	if c.BaseModel == "" {
		return "", fmt.Errorf("base model is not configured")
	}
	return c.BaseModel, nil
}
