package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// ProfileRequest represents the request body for updating a profile.
type ProfileRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
}

// ProfileResponse represents an agent's profile.
type ProfileResponse struct {
	PublicKey   string `json:"public_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// HandleUpdateProfile handles PUT /api/v1/profile.
func (h *Handlers) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())

	// Read body from cache
	body := GetCachedBody(r.Context())
	if body == nil {
		writeBadRequest(w, "Request body is required")
		return
	}

	var req ProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, "Invalid JSON: "+err.Error())
		return
	}

	if req.Name == "" && req.Description == "" {
		writeBadRequest(w, "At least name or description is required")
		return
	}

	profile, err := h.service.UpdateProfile(r.Context(), agentID, req.Name, req.Description, req.URL)
	if err != nil {
		writeInternalError(w, "Failed to update profile: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ProfileResponse{
		PublicKey:   profile.PublicKey,
		Name:        profile.Name,
		Description: profile.Description,
		URL:         profile.URL,
		CreatedAt:   profile.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   profile.UpdatedAt.Format(time.RFC3339),
	})
}

// HandleGetProfile handles GET /api/v1/profile/{publicKey}.
func (h *Handlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	// Extract public key from path: /api/v1/profile/{publicKey}
	publicKey, ok := pathAfterPrefix(r.URL.Path, "/api/v1/profile/")
	if !ok {
		writeBadRequest(w, "Invalid path")
		return
	}

	if publicKey == "" {
		writeBadRequest(w, "Public key is required")
		return
	}

	profile, err := h.service.GetProfile(r.Context(), publicKey)
	if err != nil {
		writeNotFound(w, "Profile not found")
		return
	}

	writeJSON(w, http.StatusOK, ProfileResponse{
		PublicKey:   profile.PublicKey,
		Name:        profile.Name,
		Description: profile.Description,
		URL:         profile.URL,
		CreatedAt:   profile.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   profile.UpdatedAt.Format(time.RFC3339),
	})
}
