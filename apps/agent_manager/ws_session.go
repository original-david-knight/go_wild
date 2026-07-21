package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
)

// RelaySession connects Docker container I/O to multiple WebSocket clients.
type RelaySession struct {
	agentID       string
	attach        *dockermgr.AttachSession
	mu            sync.RWMutex
	clients       map[*websocket.Conn]bool
	input         chan []byte
	done          chan struct{}
	closeOnce     sync.Once
	lineBuf       []byte // buffer for incomplete lines
	service       *AgentService
	responseBuf   string                   // accumulates response chunks for chat history
	runtimeStatus *agentdata.RuntimeStatus // cached runtime status for REST replay
}

// AddClient registers a WebSocket client.
func (rs *RelaySession) AddClient(conn *websocket.Conn) {
	rs.mu.Lock()
	rs.clients[conn] = true
	hasStatus := rs.runtimeStatus != nil
	rs.mu.Unlock()

	log.Printf("Client connected to agent %s (total: %d)", rs.agentID, len(rs.clients))

	// If we don't have a cached runtime status yet, ask the agent for one.
	// The client will fetch it via REST after onopen.
	if !hasStatus {
		rs.requestRuntimeStatus()
	}

	// Start read pump for this client
	go rs.readPump(conn)
}

// requestRuntimeStatus asks the agent to emit its full runtime status.
func (rs *RelaySession) requestRuntimeStatus() {
	req := agentdata.StatusRequestMessage{
		Type: "status_request",
		Name: "runtime_status",
	}
	cmdJSON, err := json.Marshal(req)
	if err != nil {
		return
	}
	if err := rs.writeToAgent(string(cmdJSON) + "\n"); err != nil {
		log.Printf("Failed to request runtime status for %s: %v", rs.agentID, err)
	}
}

// GetRuntimeStatus returns a copy of the cached runtime status (nil if unknown).
func (rs *RelaySession) GetRuntimeStatus() *agentdata.RuntimeStatus {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	if rs.runtimeStatus == nil {
		return nil
	}
	cp := *rs.runtimeStatus
	return &cp
}

// RemoveClient unregisters a WebSocket client.
func (rs *RelaySession) RemoveClient(conn *websocket.Conn) {
	rs.mu.Lock()
	delete(rs.clients, conn)
	clientCount := len(rs.clients)
	rs.mu.Unlock()

	conn.Close()
	log.Printf("Client disconnected from agent %s (remaining: %d)", rs.agentID, clientCount)
}

// close closes the Docker attach session and all clients.
func (rs *RelaySession) close() {
	rs.closeOnce.Do(func() {
		close(rs.done)
		close(rs.input)

		// Close Docker connection
		if rs.attach != nil && rs.attach.Conn.Conn != nil {
			rs.attach.Conn.Close()
		}

		// Close all WebSocket clients
		rs.mu.Lock()
		for conn := range rs.clients {
			conn.Close()
		}
		rs.clients = make(map[*websocket.Conn]bool)
		rs.mu.Unlock()

		if rs.service != nil && rs.agentID != "" {
			queue := newLocalA2AQueue(rs.service.db)
			requeued, err := queue.RequeueAgentClaims(context.Background(), rs.agentID)
			if err != nil {
				log.Printf("Relay session close: failed to requeue claimed jobs for %s: %v", rs.agentID, err)
			} else if requeued > 0 {
				log.Printf("Relay session close: requeued %d claimed job(s) for %s", requeued, rs.agentID)
			}
		}

		log.Printf("Relay session closed for agent %s", rs.agentID)
	})
}

// writeToAgent sends text to the container stdin.
func (rs *RelaySession) writeToAgent(text string) error {
	select {
	case rs.input <- []byte(text):
		return nil
	case <-rs.done:
		return fmt.Errorf("session closed")
	default:
		return fmt.Errorf("input channel full")
	}
}
