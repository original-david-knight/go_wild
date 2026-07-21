package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Lifecycle handlers

func (h *Handlers) startAgent(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), id)

	if status == "running" {
		// Reconcile workers even if container is already running.
		if h.workerManager != nil {
			go h.workerManager.StartAgent(id)
		}
		go h.deliverQueuedCompanyMethodJobs(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "already running"})
		return
	}

	volExists, err := h.docker.VolumeExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check volume: "+err.Error())
		return
	}
	if !volExists {
		writeError(w, http.StatusConflict, "agent volume missing; refusing to start")
		return
	}

	if err := h.docker.EnsureImage(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build image: "+err.Error())
		return
	}

	// Always remove old container so we pick up the latest image and config
	if status != "" {
		_ = h.docker.RemoveContainer(r.Context(), id)
	}

	if err := h.docker.CreateContainer(r.Context(), buildContainerCreateConfig(agent, h.brokerSecret)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create container: "+err.Error())
		return
	}
	if err := h.docker.StartContainer(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start: "+err.Error())
		return
	}

	// Start background workers for this agent
	if h.workerManager != nil {
		go h.workerManager.StartAgent(id)
	}
	go h.requeueAndDeliverJobs(id)

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (h *Handlers) stopAgent(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Stop background workers before stopping container
	if h.workerManager != nil {
		h.workerManager.StopAgent(id)
	}

	h.hub.CloseSession(id)

	if err := h.docker.StopContainer(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stop: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (h *Handlers) restartAgent(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Stop workers before restarting container
	if h.workerManager != nil {
		h.workerManager.StopAgent(id)
	}

	h.hub.CloseSession(id)

	volExists, err := h.docker.VolumeExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check volume: "+err.Error())
		return
	}
	if !volExists {
		writeError(w, http.StatusConflict, "agent volume missing; refusing to restart")
		return
	}

	if err := h.docker.EnsureImage(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build image: "+err.Error())
		return
	}

	// Remove old container (stops it first if running)
	if err := h.docker.RemoveContainer(r.Context(), id); err != nil {
		if h.docker.ContainerStatus(r.Context(), id) != "" {
			writeError(w, http.StatusInternalServerError, "failed to remove: "+err.Error())
			return
		}
	}

	// Get latest agent config from database
	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Recreate container with latest config
	if err := h.docker.CreateContainer(r.Context(), buildContainerCreateConfig(agent, h.brokerSecret)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create container: "+err.Error())
		return
	}
	if err := h.docker.StartContainer(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start: "+err.Error())
		return
	}

	// Restart background workers
	if h.workerManager != nil {
		go h.workerManager.StartAgent(id)
	}
	go h.requeueAndDeliverJobs(id)

	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

func (h *Handlers) refreshAgentImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	volExists, err := h.docker.VolumeExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check volume: "+err.Error())
		return
	}
	if !volExists {
		writeError(w, http.StatusConflict, "agent volume missing; refusing to refresh")
		return
	}

	if err := h.docker.BuildImage(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build image: "+err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), id)
	wasRunning := status == "running"
	if status != "" {
		if h.workerManager != nil {
			h.workerManager.StopAgent(id)
		}
		h.hub.CloseSession(id)
		if err := h.docker.RemoveContainer(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove: "+err.Error())
			return
		}
		if err := h.docker.CreateContainer(r.Context(), buildContainerCreateConfig(agent, h.brokerSecret)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create container: "+err.Error())
			return
		}
		if wasRunning {
			if err := h.docker.StartContainer(r.Context(), id); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to start: "+err.Error())
				return
			}
			if h.workerManager != nil {
				go h.workerManager.StartAgent(id)
			}
			go h.requeueAndDeliverJobs(id)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (h *Handlers) deliverQueuedCompanyMethodJobs(agentID string) {
	if h == nil || h.service == nil || strings.TrimSpace(agentID) == "" {
		return
	}
	if h.jobDeliveryFunc == nil {
		return
	}
	delivered, err := h.jobDeliveryFunc(context.Background(), agentID, localA2ADefaultClaimBatch)
	if err != nil {
		log.Printf("Company method: failed queued delivery for %s: %v", agentID, err)
		return
	}
	if delivered > 0 {
		log.Printf("Company method: delivered %d queued job(s) for %s", delivered, agentID)
	}
}

// requeueAndDeliverJobs requeues all claimed jobs for the agent (from before
// the restart) back to queued, then claims and delivers them along with any
// other queued jobs. Call this on agent start/restart instead of
// deliverQueuedCompanyMethodJobs so previously claimed work is not lost.
func (h *Handlers) requeueAndDeliverJobs(agentID string) {
	if h == nil || h.service == nil || strings.TrimSpace(agentID) == "" {
		return
	}
	queue := newLocalA2AQueue(h.service.db)
	requeued, err := queue.RequeueAgentClaims(context.Background(), agentID)
	if err != nil {
		log.Printf("Company method: failed to requeue claimed jobs for %s: %v", agentID, err)
	} else if requeued > 0 {
		log.Printf("Company method: requeued %d previously claimed job(s) for %s", requeued, agentID)
	}
	h.deliverQueuedCompanyMethodJobs(agentID)
}

func (h *Handlers) getAgentLogs(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tail := 200
	if t := r.URL.Query().Get("tail"); t != "" {
		if n, err := strconv.Atoi(t); err == nil {
			tail = n
		}
	}

	logs, err := h.docker.ContainerLogs(r.Context(), id, tail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get logs: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}
