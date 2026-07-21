package main

import (
	"context"
	"log"
	"net/http"
)

// Docker management handlers

func (h *Handlers) handleDockerBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := h.docker.BuildImage(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "build failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "built"})
}

func (h *Handlers) applyImageStatus(ctx context.Context, agentID string, resp *AgentResponse) {
	if h.docker == nil {
		return
	}
	stale, imageID, desiredID, err := h.docker.AgentImageStale(ctx, agentID)
	if err != nil {
		log.Printf("Failed to check image status for %s: %v", agentID, err)
		return
	}
	resp.ImageStale = stale
	resp.ImageBuildID = imageID
	resp.DesiredBuildID = desiredID
}

func (h *Handlers) handleDockerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	imageExists := h.docker.ImageExists(r.Context())

	_, err := h.docker.Ping(r.Context())
	dockerAvailable := err == nil

	writeJSON(w, http.StatusOK, map[string]any{
		"docker_available": dockerAvailable,
		"image_exists":     imageExists,
	})
}
