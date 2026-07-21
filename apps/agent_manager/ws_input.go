package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
)

type uiMessageHandlerFunc func(rs *RelaySession, conn *websocket.Conn, message []byte) error
type controlActionHandlerFunc func(result *CommandResultMessage)

var uiMessageHandlers = map[string]uiMessageHandlerFunc{
	"prompt": func(rs *RelaySession, _ *websocket.Conn, message []byte) error {
		var pm PromptMessage
		if err := json.Unmarshal(message, &pm); err != nil {
			return fmt.Errorf("invalid prompt message: %w", err)
		}
		rs.saveChatMessageAsync("user", pm.Text)
		return rs.writeToAgent(pm.Text + "\r\n")
	},
	"command": func(rs *RelaySession, _ *websocket.Conn, message []byte) error {
		var cm agentdata.CommandMessage
		if err := json.Unmarshal(message, &cm); err != nil {
			return fmt.Errorf("invalid command message: %w", err)
		}
		cmdJSON, err := json.Marshal(cm)
		if err != nil {
			return fmt.Errorf("error marshaling command: %w", err)
		}
		return rs.writeToAgent(string(cmdJSON) + "\n")
	},
	"control": func(rs *RelaySession, conn *websocket.Conn, message []byte) error {
		var ctrl ControlMessage
		if err := json.Unmarshal(message, &ctrl); err != nil {
			return fmt.Errorf("invalid control message: %w", err)
		}
		return rs.handleControlMessage(conn, ctrl)
	},
	"input": func(rs *RelaySession, _ *websocket.Conn, message []byte) error {
		return rs.handleLegacyInput(message)
	},
	"resize": func(_ *RelaySession, _ *websocket.Conn, message []byte) error {
		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return fmt.Errorf("invalid resize message: %w", err)
		}
		log.Printf("Resize request: cols=%d rows=%d", msg.Cols, msg.Rows)
		return nil
	},
}

var controlActionHandlers = map[string]controlActionHandlerFunc{
	"ping": func(result *CommandResultMessage) {
		result.Message = "pong"
	},
}

func isUIMessageType(msgType string) bool {
	_, ok := uiMessageHandlers[msgType]
	return ok
}

func isControlAction(action string) bool {
	_, ok := controlActionHandlers[action]
	return ok
}

// relayInput reads from input channel and writes to container stdin.
func (rs *RelaySession) relayInput() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("relayInput panic for %s: %v\n%s", rs.agentID, r, debug.Stack())
			rs.close()
		}
	}()

	for {
		select {
		case <-rs.done:
			return
		case data, ok := <-rs.input:
			if !ok {
				return
			}

			// Nil check to prevent panic during session cleanup
			if rs.attach == nil || rs.attach.Conn.Conn == nil {
				log.Printf("relayInput: nil attachment for %s, exiting", rs.agentID)
				return
			}
			if _, err := rs.attach.Conn.Conn.Write(data); err != nil {
				log.Printf("Error writing to container %s: %v", rs.agentID, err)
				rs.close()
				return
			}
		}
	}
}

// readPump reads from one WebSocket client and sends input to the container.
func (rs *RelaySession) readPump(conn *websocket.Conn) {
	defer rs.RemoveClient(conn)

	// Set pong handler
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// Start ping ticker
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	// Ping goroutine
	go func() {
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-rs.done:
				return
			}
		}
	}()

	for {
		select {
		case <-rs.done:
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			return
		}

		// Handle structured UI messages
		if err := rs.handleUIMessage(conn, message); err != nil {
			log.Printf("Error handling UI message: %v", err)
		}
	}
}

// handleUIMessage routes incoming messages based on their type.
func (rs *RelaySession) handleUIMessage(conn *websocket.Conn, message []byte) error {
	var base UIMessage
	if err := json.Unmarshal(message, &base); err != nil {
		return rs.handleLegacyInput(message)
	}

	if !isUIMessageType(base.Type) {
		return rs.handleLegacyInput(message)
	}
	handler, ok := uiMessageHandlers[base.Type]
	if !ok {
		return rs.handleLegacyInput(message)
	}
	return handler(rs, conn, message)
}

// handleLegacyInput handles the old base64-encoded input format for backward compatibility.
func (rs *RelaySession) handleLegacyInput(message []byte) error {
	var msg WSMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		return fmt.Errorf("invalid message format: %w", err)
	}

	if msg.Type != "input" {
		return nil // ignore unknown types
	}

	data, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return fmt.Errorf("error decoding input: %w", err)
	}

	return rs.writeToAgent(string(data))
}

// handleControlMessage handles container lifecycle control messages.
func (rs *RelaySession) handleControlMessage(conn *websocket.Conn, ctrl ControlMessage) error {
	result := CommandResultMessage{
		Type:    "command_result",
		Command: "control:" + ctrl.Action,
		Success: true,
	}

	if !isControlAction(ctrl.Action) {
		// Future: implement start/stop/restart here
		result.Success = false
		result.Message = "control action not yet implemented: " + ctrl.Action
		return rs.sendCommandResult(conn, result)
	}
	handler, ok := controlActionHandlers[ctrl.Action]
	if !ok {
		result.Success = false
		result.Message = "control action not yet implemented: " + ctrl.Action
		return rs.sendCommandResult(conn, result)
	}
	handler(&result)
	return rs.sendCommandResult(conn, result)
}

// sendCommandResult sends a command result message to a specific client.
func (rs *RelaySession) sendCommandResult(conn *websocket.Conn, result CommandResultMessage) error {
	msgBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("error marshaling result: %w", err)
	}

	conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}
