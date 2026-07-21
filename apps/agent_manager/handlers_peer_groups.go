package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type peerGroupCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type peerGroupHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, groupID string)
type peerGroupMemberHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, groupID, agentID string)

var peerGroupCollectionHandlers = map[string]peerGroupCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listPeerGroups(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createPeerGroup(w, r)
	},
}

var peerGroupHandlers = map[string]peerGroupHandlerFunc{
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, groupID string) {
		h.deletePeerGroup(w, r, groupID)
	},
}

var peerGroupMembersHandlers = map[string]peerGroupHandlerFunc{
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, groupID string) {
		h.addGroupMember(w, r, groupID)
	},
}

var peerGroupMemberHandlers = map[string]peerGroupMemberHandlerFunc{
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, groupID, agentID string) {
		h.removeGroupMember(w, r, groupID, agentID)
	},
}

func isPeerGroupCollectionMethod(method string) bool {
	_, ok := peerGroupCollectionHandlers[method]
	return ok
}

func isPeerGroupMethod(method string) bool {
	_, ok := peerGroupHandlers[method]
	return ok
}

func isPeerGroupMembersMethod(method string) bool {
	_, ok := peerGroupMembersHandlers[method]
	return ok
}

func isPeerGroupMemberMethod(method string) bool {
	_, ok := peerGroupMemberHandlers[method]
	return ok
}

// Peer Group handlers

// handlePeerGroups handles GET /api/peer-groups and POST /api/peer-groups.
func (h *Handlers) handlePeerGroups(w http.ResponseWriter, r *http.Request) {
	if !isPeerGroupCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := peerGroupCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r)
}

// handlePeerGroup routes /api/peer-groups/{id} and sub-paths.
func (h *Handlers) handlePeerGroup(w http.ResponseWriter, r *http.Request) {
	route, err := parsePeerGroupRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if route.action == "" {
		if !isPeerGroupMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := peerGroupHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, route.groupID)
		return
	}
	if route.action != "members" {
		writeError(w, http.StatusNotFound, "unknown peer group action")
		return
	}
	if route.agentID == "" {
		if !isPeerGroupMembersMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := peerGroupMembersHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, route.groupID)
		return
	}
	if !isPeerGroupMemberMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := peerGroupMemberHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, route.groupID, route.agentID)
}

func (h *Handlers) listPeerGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	groups, err := h.service.ListPeerGroups(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list peer groups: "+err.Error())
		return
	}

	result := make([]map[string]any, len(groups))
	for i, g := range groups {
		members, _ := h.service.GetGroupMembers(ctx, g.ID)
		memberList := make([]map[string]any, len(members))
		for j, m := range members {
			agentName := m.AgentID
			if agent, err := h.service.GetAgent(ctx, m.AgentID); err == nil {
				agentName = agent.Name
			}
			memberList[j] = map[string]any{
				"agent_id": m.AgentID,
				"name":     agentName,
			}
		}
		result[i] = map[string]any{
			"id":         g.ID,
			"name":       g.Name,
			"members":    memberList,
			"created_at": g.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"groups": result})
}

func (h *Handlers) createPeerGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	group, err := h.service.CreatePeerGroup(r.Context(), req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create peer group: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         group.ID,
		"name":       group.Name,
		"members":    []any{},
		"created_at": group.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handlers) deletePeerGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	// Get current members before deleting so we can update their tools
	members, _ := h.service.GetGroupMembers(r.Context(), groupID)

	if err := h.service.DeletePeerGroup(r.Context(), groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete peer group: "+err.Error())
		return
	}

	// Auto-disable messaging for former members who have no remaining peer groups
	for _, m := range members {
		if err := h.service.EnsureMessagingToolDisabled(r.Context(), m.AgentID); err != nil {
			log.Printf("Warning: failed to auto-disable messaging tools for %s: %v", m.AgentID, err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) addGroupMember(w http.ResponseWriter, r *http.Request, groupID string) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	if err := h.service.AddAgentToGroup(r.Context(), groupID, req.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member: "+err.Error())
		return
	}

	// Auto-enable messaging tool group for this agent
	if err := h.service.EnsureMessagingToolEnabled(r.Context(), req.AgentID); err != nil {
		log.Printf("Warning: failed to auto-enable messaging tools for %s: %v", req.AgentID, err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (h *Handlers) removeGroupMember(w http.ResponseWriter, r *http.Request, groupID, agentID string) {
	if err := h.service.RemoveAgentFromGroup(r.Context(), groupID, agentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member: "+err.Error())
		return
	}

	// Auto-disable messaging tool group if agent has no remaining peer groups
	if err := h.service.EnsureMessagingToolDisabled(r.Context(), agentID); err != nil {
		log.Printf("Warning: failed to auto-disable messaging tools for %s: %v", agentID, err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
