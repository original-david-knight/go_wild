package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"runtime/debug"
	"time"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
)

type agentMessageHistoryHandlerFunc func(rs *RelaySession, agentMsg AgentMessage)
type runtimeStatusUpdaterFunc func(rs *RelaySession, agentMsg AgentMessage, raw []byte)

var agentMessageHistoryHandlers = map[string]agentMessageHistoryHandlerFunc{
	"response": func(rs *RelaySession, agentMsg AgentMessage) {
		rs.responseBuf += agentMsg.Content
	},
	"response_end": func(rs *RelaySession, _ AgentMessage) {
		rs.saveChatMessageAsync("assistant", rs.responseBuf)
		rs.responseBuf = ""
	},
	"content": func(rs *RelaySession, agentMsg AgentMessage) {
		// Save alt text to chat history (not the full binary payload)
		alt := "[" + agentMsg.ContentType + "] " + agentMsg.Detail
		rs.saveChatMessageAsync("assistant", alt)
	},
}

var runtimeStatusUpdaters = map[string]runtimeStatusUpdaterFunc{
	"prompt": func(rs *RelaySession, _ AgentMessage, _ []byte) {
		rs.runtimeStatus.State = "idle"
	},
	"thinking": func(rs *RelaySession, _ AgentMessage, _ []byte) {
		rs.runtimeStatus.State = "thinking"
	},
	"response": func(rs *RelaySession, _ AgentMessage, _ []byte) {
		rs.runtimeStatus.State = "responding"
	},
	"tool_call": func(rs *RelaySession, _ AgentMessage, _ []byte) {
		rs.runtimeStatus.State = "tool_running"
	},
	"smart_mode": func(rs *RelaySession, agentMsg AgentMessage, _ []byte) {
		rs.runtimeStatus.SmartMode = agentMsg.Content == "on"
		if agentMsg.Detail != "" {
			rs.runtimeStatus.Model = agentMsg.Detail
		}
	},
	"runtime_status": func(rs *RelaySession, _ AgentMessage, raw []byte) {
		// Full status snapshot from agent — replace cache entirely.
		var full agentdata.RuntimeStatus
		if err := json.Unmarshal(raw, &full); err != nil {
			log.Printf("Invalid runtime_status JSON for %s: %v", rs.agentID, err)
			return
		}
		rs.runtimeStatus = &full
	},
}

func applyAgentMessageHistory(rs *RelaySession, agentMsg AgentMessage) {
	handler, ok := agentMessageHistoryHandlers[agentMsg.Type]
	if !ok {
		return
	}
	handler(rs, agentMsg)
}

func applyRuntimeStatusUpdate(rs *RelaySession, agentMsg AgentMessage, raw []byte) {
	updater, ok := runtimeStatusUpdaters[agentMsg.Type]
	if !ok {
		return
	}
	updater(rs, agentMsg, raw)
}

func (rs *RelaySession) saveChatMessageAsync(role, content string) {
	if rs.service == nil || content == "" {
		return
	}

	service := rs.service
	agentID := rs.agentID
	go func(role, content string) {
		if err := service.SaveChatMessage(context.Background(), agentID, role, content); err != nil {
			log.Printf("Error saving chat message for %s (%s): %v", agentID, role, err)
		}
	}(role, content)
}

// relayOutput reads from container stdout and broadcasts to all clients.
func (rs *RelaySession) relayOutput() {
	defer rs.close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("relayOutput panic for %s: %v\n%s", rs.agentID, r, debug.Stack())
		}
	}()

	buf := make([]byte, 8192)
	for {
		select {
		case <-rs.done:
			return
		default:
		}

		// Nil check to prevent panic during session cleanup
		if rs.attach == nil || rs.attach.Conn.Reader == nil {
			log.Printf("relayOutput: nil attachment for %s, exiting", rs.agentID)
			return
		}

		n, err := rs.attach.Conn.Reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from container %s: %v", rs.agentID, err)
			}

			// Send exit status to clients
			rs.broadcastStatus("exited", "Container stopped")
			return
		}

		if n > 0 {
			// Append to line buffer
			rs.lineBuf = append(rs.lineBuf, buf[:n]...)

			// Process complete lines
			rs.processLines()
		}
	}
}

// processLines extracts complete lines from the buffer and broadcasts them.
func (rs *RelaySession) processLines() {
	for {
		// Find newline
		nlIdx := -1
		for i, b := range rs.lineBuf {
			if b == '\n' {
				nlIdx = i
				break
			}
		}

		if nlIdx < 0 {
			// No complete line yet
			return
		}

		// Extract the line (without newline)
		line := rs.lineBuf[:nlIdx]
		rs.lineBuf = rs.lineBuf[nlIdx+1:]

		// Handle \r\n
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		// Trim leading whitespace and control characters for JSON detection
		trimmedLine := bytes.TrimLeft(line, " \t\b\x00")

		// Try to parse as agent JSON message
		if agentWSMsg, ok := rs.tryParseAgentJSON(trimmedLine); ok {
			rs.broadcastMessage(agentWSMsg)
			continue
		}

		// Check for JSON embedded after ANSI sequences (e.g. "you>" prompt on same line)
		if jsonIdx := bytes.IndexByte(line, '{'); jsonIdx > 0 {
			prefix := line[:jsonIdx]
			jsonPart := line[jsonIdx:]
			if agentWSMsg, ok := rs.tryParseAgentJSON(jsonPart); ok {
				// Send the prefix as raw output, then the structured message
				rawData := append(prefix, '\n')
				rs.broadcastMessage(WSMessage{
					Type: "output",
					Data: base64.StdEncoding.EncodeToString(rawData),
				})
				rs.broadcastMessage(agentWSMsg)
				continue
			}
		}

		// Not a valid agent message - send as raw output (include newline)
		rawData := append(line, '\n')
		wsMsg := WSMessage{
			Type: "output",
			Data: base64.StdEncoding.EncodeToString(rawData),
		}
		rs.broadcastMessage(wsMsg)
	}
}

// tryParseAgentJSON attempts to parse data as an agent JSON message and returns a WSMessage.
func (rs *RelaySession) tryParseAgentJSON(data []byte) (WSMessage, bool) {
	if len(data) == 0 || data[0] != '{' {
		return WSMessage{}, false
	}
	var agentMsg AgentMessage
	if err := json.Unmarshal(data, &agentMsg); err != nil || agentMsg.Type == "" {
		return WSMessage{}, false
	}

	// Accumulate response chunks for chat history
	applyAgentMessageHistory(rs, agentMsg)

	wsMsg := WSMessage{
		Type:        "agent",
		AgentType:   agentMsg.Type,
		Content:     agentMsg.Content,
		ContentType: agentMsg.ContentType,
		Name:        agentMsg.Name,
		Detail:      agentMsg.Detail,
		Status:      agentMsg.Status,
		Tokens:      agentMsg.Tokens,
	}

	// Update cached runtime status incrementally from individual messages.
	rs.mu.Lock()
	if rs.runtimeStatus == nil {
		rs.runtimeStatus = &agentdata.RuntimeStatus{Type: "runtime_status"}
	}
	applyRuntimeStatusUpdate(rs, agentMsg, data)
	rs.mu.Unlock()

	return wsMsg, true
}

// broadcastMessage sends a WebSocket message to all clients.
func (rs *RelaySession) broadcastMessage(msg WSMessage) {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	rs.mu.RLock()
	defer rs.mu.RUnlock()

	for conn := range rs.clients {
		conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			log.Printf("Error writing to client: %v", err)
		}
	}
}

// broadcastStatus sends a status message to all clients.
func (rs *RelaySession) broadcastStatus(status, message string) {
	msg := WSMessage{
		Type:    "status",
		Status:  status,
		Message: message,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling status message: %v", err)
		return
	}

	rs.mu.RLock()
	defer rs.mu.RUnlock()

	for conn := range rs.clients {
		conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			log.Printf("Error writing status to client: %v", err)
		}
	}
}
