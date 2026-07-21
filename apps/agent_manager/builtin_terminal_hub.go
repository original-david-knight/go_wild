package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const builtinTerminalBacklogLimit = 400

type BuiltinTerminalHub struct {
	mu         sync.RWMutex
	writeMu    sync.Mutex
	clients    map[*websocket.Conn]struct{}
	backlog    []WSMessage
	maxBacklog int
}

func newBuiltinTerminalHub() *BuiltinTerminalHub {
	return &BuiltinTerminalHub{
		clients:    make(map[*websocket.Conn]struct{}),
		maxBacklog: builtinTerminalBacklogLimit,
	}
}

func (h *BuiltinTerminalHub) Snapshot() []WSMessage {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.backlog) == 0 {
		return nil
	}
	out := make([]WSMessage, len(h.backlog))
	copy(out, h.backlog)
	return out
}

func (h *BuiltinTerminalHub) PublishText(text string) {
	if h == nil || text == "" {
		return
	}
	h.Publish(WSMessage{
		Type: "output",
		Data: base64.StdEncoding.EncodeToString([]byte(text)),
	})
}

func (h *BuiltinTerminalHub) PublishStatus(status, message string) {
	if h == nil {
		return
	}
	h.Publish(WSMessage{
		Type:    "status",
		Status:  status,
		Message: message,
	})
}

func (h *BuiltinTerminalHub) Publish(msg WSMessage) {
	if h == nil {
		return
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Builtin terminal: marshal failed: %v", err)
		return
	}

	h.mu.Lock()
	h.backlog = append(h.backlog, msg)
	if limit := h.maxBacklog; limit > 0 && len(h.backlog) > limit {
		h.backlog = append([]WSMessage(nil), h.backlog[len(h.backlog)-limit:]...)
	}
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.Unlock()

	var failed []*websocket.Conn
	for _, conn := range clients {
		if err := h.writeMessage(conn, websocket.TextMessage, msgBytes); err != nil {
			log.Printf("Builtin terminal: client write failed: %v", err)
			failed = append(failed, conn)
		}
	}
	if len(failed) == 0 {
		return
	}

	h.mu.Lock()
	for _, conn := range failed {
		delete(h.clients, conn)
		_ = conn.Close()
	}
	h.mu.Unlock()
}

func (h *BuiltinTerminalHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		writeError(w, http.StatusServiceUnavailable, "builtin terminal stream unavailable")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Builtin terminal websocket upgrade failed: %v", err)
		return
	}

	if err := h.writeJSON(conn, WSMessage{Type: "status", Status: "running", Message: "builtin method stream connected"}); err != nil {
		log.Printf("Builtin terminal initial status write failed: %v", err)
		_ = conn.Close()
		return
	}

	for _, msg := range h.Snapshot() {
		if err := h.writeJSON(conn, msg); err != nil {
			log.Printf("Builtin terminal backlog replay failed: %v", err)
			_ = conn.Close()
			return
		}
	}

	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *BuiltinTerminalHub) writeJSON(conn *websocket.Conn, msg WSMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return h.writeMessage(conn, websocket.TextMessage, msgBytes)
}

func (h *BuiltinTerminalHub) writeMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return conn.WriteMessage(messageType, payload)
}
