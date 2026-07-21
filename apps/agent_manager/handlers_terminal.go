package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// WebSocket terminal handler

func (h *Handlers) handleTerminal(w http.ResponseWriter, r *http.Request, agentID string) {
	status := h.docker.ContainerStatus(r.Context(), agentID)
	if status != "running" {
		writeError(w, http.StatusBadRequest, "container is not running (status: "+status+")")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	session, err := h.hub.GetOrCreateSession(agentID)
	if err != nil {
		log.Printf("Failed to create relay session: %v", err)
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
		conn.Close()
		return
	}

	statusMsg, _ := json.Marshal(WSMessage{
		Type:   "status",
		Status: "running",
	})
	conn.WriteMessage(websocket.TextMessage, statusMsg)

	session.AddClient(conn)
	go h.deliverQueuedCompanyMethodJobs(agentID)
}
