package main

import (
	"encoding/json"
	"net/http"
)

type emailWhitelistHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string)

var emailWhitelistHandlers = map[string]emailWhitelistHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.getEmailWhitelist(w, r, agentID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.addEmailWhitelistEntry(w, r, agentID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.removeEmailWhitelistEntry(w, r, agentID)
	},
}

func isEmailWhitelistMethod(method string) bool {
	_, ok := emailWhitelistHandlers[method]
	return ok
}

func (h *Handlers) handleEmailWhitelist(w http.ResponseWriter, r *http.Request, agentID string) {
	if !isEmailWhitelistMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := emailWhitelistHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, agentID)
}

func (h *Handlers) getEmailWhitelist(w http.ResponseWriter, r *http.Request, agentID string) {
	whitelist, err := h.service.GetEmailWhitelist(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"emails": []string{}})
		return
	}
	if whitelist == nil {
		whitelist = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"emails": whitelist})
}

func (h *Handlers) addEmailWhitelistEntry(w http.ResponseWriter, r *http.Request, agentID string) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := h.service.AddEmailWhitelistEntry(r.Context(), agentID, req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add whitelist entry: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "added", "email": req.Email})
}

func (h *Handlers) removeEmailWhitelistEntry(w http.ResponseWriter, r *http.Request, agentID string) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := h.service.RemoveEmailWhitelistEntry(r.Context(), agentID, req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove whitelist entry: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "email": req.Email})
}
