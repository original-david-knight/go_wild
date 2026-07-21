package main

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsPingInterval = 30 * time.Second
	wsPongWait     = 60 * time.Second
	wsWriteWait    = 10 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSMessage is the JSON message format for WebSocket communication.
type WSMessage struct {
	Type    string `json:"type"`              // "output", "input", "resize", "status", "agent"
	Data    string `json:"data,omitempty"`    // base64-encoded terminal data (for "output")
	Status  string `json:"status,omitempty"`  // "running", "exited", "error" (for "status")
	Message string `json:"message,omitempty"` // status message
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	// Agent message fields (for "agent" type)
	AgentType   string `json:"agent_type,omitempty"` // "prompt", "system", "response", etc.
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"` // MIME type for "content" messages
	Name        string `json:"name,omitempty"`         // tool name
	Detail      string `json:"detail,omitempty"`       // tool detail
	Tokens      int    `json:"tokens,omitempty"`
}

// UIMessage is the envelope for structured messages from the UI.
type UIMessage struct {
	Type string `json:"type"` // "prompt", "command", "control", "input" (legacy)
}

// PromptMessage is a user text prompt to forward to the agent.
type PromptMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CommandMessage is agentdata.CommandMessage — a parsed slash command from the UI.

// ControlMessage is a container lifecycle control message.
type ControlMessage struct {
	Type   string `json:"type"`
	Action string `json:"action"` // "start", "stop", "restart", "ping"
}

// CommandResultMessage is the result of a manager-handled command.
type CommandResultMessage struct {
	Type    string         `json:"type"` // "command_result"
	Command string         `json:"command"`
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// AgentMessage is the JSON format emitted by the agent.
type AgentMessage struct {
	Type        string `json:"type"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Name        string `json:"name,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Status      string `json:"status,omitempty"`
	Tokens      int    `json:"tokens,omitempty"`
}
