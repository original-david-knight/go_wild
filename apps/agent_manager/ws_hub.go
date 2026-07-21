package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
)

// SessionHub manages active relay sessions (one per agent).
type SessionHub struct {
	mu       sync.RWMutex
	sessions map[string]*RelaySession // agentID -> session
	docker   *dockermgr.DockerManager
	service  *AgentService
}

// NewSessionHub creates a new SessionHub.
func NewSessionHub(docker *dockermgr.DockerManager, service *AgentService) *SessionHub {
	return &SessionHub{
		sessions: make(map[string]*RelaySession),
		docker:   docker,
		service:  service,
	}
}

// GetOrCreateSession gets an existing session or creates a new one.
func (h *SessionHub) GetOrCreateSession(agentID string) (*RelaySession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if session exists
	if session, exists := h.sessions[agentID]; exists {
		// Verify session is still alive
		select {
		case <-session.done:
			// Session is closed, remove it and create new one
			delete(h.sessions, agentID)
		default:
			// Session is alive, return it
			return session, nil
		}
	}

	// Create new session
	attach, err := h.docker.AttachContainer(context.Background(), agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to attach to container: %w", err)
	}

	session := &RelaySession{
		agentID: agentID,
		attach:  attach,
		clients: make(map[*websocket.Conn]bool),
		input:   make(chan []byte, 256),
		done:    make(chan struct{}),
		service: h.service,
	}

	// Start relay goroutines
	go session.relayOutput()
	go session.relayInput()

	h.sessions[agentID] = session
	return session, nil
}

// GetSession returns the relay session for an agent (nil if none exists).
func (h *SessionHub) GetSession(agentID string) *RelaySession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	session, exists := h.sessions[agentID]
	if !exists {
		return nil
	}
	// Verify session is still alive
	select {
	case <-session.done:
		return nil
	default:
		return session
	}
}

// CloseSession closes and removes a session.
func (h *SessionHub) CloseSession(agentID string) {
	h.mu.Lock()
	session, exists := h.sessions[agentID]
	if exists {
		delete(h.sessions, agentID)
	}
	h.mu.Unlock()

	if exists {
		session.close()
	}
}

// CloseAll closes all sessions (for shutdown).
func (h *SessionHub) CloseAll() {
	h.mu.Lock()
	sessions := make([]*RelaySession, 0, len(h.sessions))
	for _, session := range h.sessions {
		sessions = append(sessions, session)
	}
	h.sessions = make(map[string]*RelaySession)
	h.mu.Unlock()

	for _, session := range sessions {
		session.close()
	}
}
