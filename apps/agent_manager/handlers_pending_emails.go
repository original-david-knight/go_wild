package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type pendingEmailActionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string)

type pendingEmailActionRoute struct {
	method  string
	handler pendingEmailActionHandlerFunc
}

var pendingEmailActionHandlers = map[string]pendingEmailActionRoute{
	"": {
		method: http.MethodGet,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
			h.listPendingEmails(w, r, agentID)
		},
	},
	"approve": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
			h.approvePendingEmails(w, r, agentID)
		},
	},
	"reject": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
			h.rejectPendingEmails(w, r, agentID)
		},
	},
}

func isPendingEmailAction(action string) bool {
	_, ok := pendingEmailActionHandlers[action]
	return ok
}

func parsePendingEmailAction(path, agentID string) string {
	prefix := "/api/agents/" + agentID + "/pending-emails"
	subAction := strings.TrimPrefix(path, prefix)
	subAction = strings.TrimPrefix(subAction, "/")
	return subAction
}

// Email Approval handlers

func (h *Handlers) handlePendingEmails(w http.ResponseWriter, r *http.Request, agentID string) {
	// Parse sub-action from URL: /api/agents/{id}/pending-emails[/approve|/reject]
	action := parsePendingEmailAction(r.URL.Path, agentID)
	if !isPendingEmailAction(action) {
		writeError(w, http.StatusNotFound, "unknown pending-emails action")
		return
	}
	route, ok := pendingEmailActionHandlers[action]
	if !ok || route.method != r.Method {
		writeError(w, http.StatusNotFound, "unknown pending-emails action")
		return
	}
	route.handler(h, w, r, agentID)
}

func (h *Handlers) listPendingEmails(w http.ResponseWriter, r *http.Request, agentID string) {
	emails, err := h.service.GetPendingEmails(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"emails": []any{}, "count": 0})
		return
	}

	result := make([]map[string]any, len(emails))
	for i, pe := range emails {
		result[i] = map[string]any{
			"id":         pe.ID,
			"type":       pe.Type,
			"recipients": pe.Recipients,
			"subject":    pe.Subject,
			"preview":    pe.Preview,
			"status":     pe.Status,
			"created_at": pe.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"emails": result, "count": len(result)})
}

func (h *Handlers) approvePendingEmails(w http.ResponseWriter, r *http.Request, agentID string) {
	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.All {
		emails, err := h.service.GetPendingEmails(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get pending emails: "+err.Error())
			return
		}
		approved := 0
		for _, pe := range emails {
			if _, err := h.service.ApprovePendingEmail(r.Context(), agentID, pe.ID); err != nil {
				log.Printf("Failed to approve email %s: %v", pe.ID, err)
				continue
			}
			approved++
		}
		writeJSON(w, http.StatusOK, map[string]any{"approved": approved})
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id or all required")
		return
	}

	pe, err := h.service.ApprovePendingEmail(r.Context(), agentID, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve email: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "approved",
		"id":         pe.ID,
		"recipients": pe.Recipients,
	})
}

func (h *Handlers) rejectPendingEmails(w http.ResponseWriter, r *http.Request, agentID string) {
	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.All {
		emails, err := h.service.GetPendingEmails(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get pending emails: "+err.Error())
			return
		}
		rejected := 0
		for _, pe := range emails {
			if _, err := h.service.RejectPendingEmail(r.Context(), agentID, pe.ID); err != nil {
				log.Printf("Failed to reject email %s: %v", pe.ID, err)
				continue
			}
			rejected++
		}
		writeJSON(w, http.StatusOK, map[string]any{"rejected": rejected})
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id or all required")
		return
	}

	pe, err := h.service.RejectPendingEmail(r.Context(), agentID, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject email: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "rejected",
		"id":         pe.ID,
		"recipients": pe.Recipients,
	})
}
