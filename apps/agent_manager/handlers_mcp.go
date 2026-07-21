package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

type mcpServerRequest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	WorkingDir  string            `json:"working_dir"`
	DefaultEnv  map[string]string `json:"default_env"`
}

type agentMCPConfigRequest struct {
	Enabled    bool              `json:"enabled"`
	Args       []string          `json:"args"`
	WorkingDir string            `json:"working_dir"`
	Env        map[string]string `json:"env"`
}

type mcpCollectionMethodHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type mcpServerMethodHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, serverID string)
type agentMCPMethodHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, serverID, action string)

var mcpCollectionMethodHandlers = map[string]mcpCollectionMethodHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listMCPServers(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createMCPServer(w, r)
	},
}

var mcpServerMethodHandlers = map[string]mcpServerMethodHandlerFunc{
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, serverID string) {
		h.updateMCPServer(w, r, serverID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, serverID string) {
		h.deleteMCPServer(w, r, serverID)
	},
}

var agentMCPMethodHandlers = map[string]agentMCPMethodHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, serverID, _ string) {
		if serverID != "" {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.listAgentMCPServers(w, r, agentID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, serverID, action string) {
		if serverID == "" {
			writeError(w, http.StatusBadRequest, "server ID required")
			return
		}
		if action == "test" {
			h.testAgentMCPServer(w, r, agentID, serverID)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, serverID, _ string) {
		if serverID == "" {
			writeError(w, http.StatusBadRequest, "server ID required")
			return
		}
		h.upsertAgentMCPServer(w, r, agentID, serverID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, serverID, _ string) {
		if serverID == "" {
			writeError(w, http.StatusBadRequest, "server ID required")
			return
		}
		h.deleteAgentMCPServer(w, r, agentID, serverID)
	},
}

func isMCPCollectionMethod(method string) bool {
	_, ok := mcpCollectionMethodHandlers[method]
	return ok
}

func isMCPServerMethod(method string) bool {
	_, ok := mcpServerMethodHandlers[method]
	return ok
}

func isAgentMCPMethod(method string) bool {
	_, ok := agentMCPMethodHandlers[method]
	return ok
}

func (h *Handlers) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if !isMCPCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := mcpCollectionMethodHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r)
}

func (h *Handlers) handleMCPServer(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "server ID required")
		return
	}
	serverID := path

	if !isMCPServerMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := mcpServerMethodHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, serverID)
}

func (h *Handlers) handleAgentMCPServers(w http.ResponseWriter, r *http.Request, agentID, serverID, action string) {
	if !isAgentMCPMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := agentMCPMethodHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, agentID, serverID, action)
}

func (h *Handlers) listMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.service.ListMCPServers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list mcp servers: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (h *Handlers) createMCPServer(w http.ResponseWriter, r *http.Request) {
	var req mcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	now := time.Now()
	server := &data.MCPServer{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Command:     req.Command,
		Args:        req.Args,
		WorkingDir:  req.WorkingDir,
		DefaultEnv:  req.DefaultEnv,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.service.CreateMCPServer(r.Context(), server); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create mcp server: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, server)
}

func (h *Handlers) updateMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	var req mcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ID != "" && req.ID != serverID {
		writeError(w, http.StatusBadRequest, "id mismatch")
		return
	}

	server, err := h.service.GetMCPServer(r.Context(), serverID)
	if err != nil || server == nil {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	}

	if req.Name != "" {
		server.Name = req.Name
	}
	if req.Description != "" {
		server.Description = req.Description
	}
	if req.Command != "" {
		server.Command = req.Command
	}
	if req.Args != nil {
		server.Args = req.Args
	}
	server.WorkingDir = req.WorkingDir
	if req.DefaultEnv != nil {
		server.DefaultEnv = req.DefaultEnv
	}
	server.UpdatedAt = time.Now()

	if err := h.service.UpdateMCPServer(r.Context(), server); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update mcp server: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, server)
}

func (h *Handlers) deleteMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	if err := h.service.DeleteMCPServer(r.Context(), serverID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete mcp server: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) listAgentMCPServers(w http.ResponseWriter, r *http.Request, agentID string) {
	configs, err := h.service.ListAgentMCPServers(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent mcp servers: "+err.Error())
		return
	}
	servers, err := h.service.ListMCPServers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list mcp servers: "+err.Error())
		return
	}
	serverByID := make(map[string]*data.MCPServer, len(servers))
	for _, s := range servers {
		serverByID[s.ID] = s
	}

	type response struct {
		ServerID          string            `json:"server_id"`
		ServerName        string            `json:"server_name,omitempty"`
		ServerDescription string            `json:"server_description,omitempty"`
		Enabled           bool              `json:"enabled"`
		Args              []string          `json:"args,omitempty"`
		WorkingDir        string            `json:"working_dir,omitempty"`
		Env               map[string]string `json:"env,omitempty"`
		CreatedAt         time.Time         `json:"created_at"`
		UpdatedAt         time.Time         `json:"updated_at"`
	}

	out := make([]response, 0, len(configs))
	for _, cfg := range configs {
		entry := response{
			ServerID:   cfg.ServerID,
			Enabled:    cfg.Enabled,
			Args:       cfg.Args,
			WorkingDir: cfg.WorkingDir,
			Env:        cfg.Env,
			CreatedAt:  cfg.CreatedAt,
			UpdatedAt:  cfg.UpdatedAt,
		}
		if s := serverByID[cfg.ServerID]; s != nil {
			entry.ServerName = s.Name
			entry.ServerDescription = s.Description
		}
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{"configs": out})
}

func (h *Handlers) upsertAgentMCPServer(w http.ResponseWriter, r *http.Request, agentID, serverID string) {
	var req agentMCPConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if _, err := h.service.GetMCPServer(r.Context(), serverID); err != nil {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	}

	cfg := &data.AgentMCPServer{
		AgentID:    agentID,
		ServerID:   serverID,
		Enabled:    req.Enabled,
		Args:       req.Args,
		WorkingDir: req.WorkingDir,
		Env:        req.Env,
	}
	if err := h.service.UpsertAgentMCPServer(r.Context(), cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent mcp config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) deleteAgentMCPServer(w http.ResponseWriter, r *http.Request, agentID, serverID string) {
	if err := h.service.DeleteAgentMCPServer(r.Context(), agentID, serverID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent mcp config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) testAgentMCPServer(w http.ResponseWriter, r *http.Request, agentID, serverID string) {
	if h.mcpHost == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp host manager unavailable")
		return
	}
	cfg, err := h.service.GetAgentMCPServer(r.Context(), agentID, serverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent mcp config: "+err.Error())
		return
	}
	if cfg == nil || !cfg.Enabled {
		writeError(w, http.StatusBadRequest, "mcp server is not enabled for this agent")
		return
	}

	toolsList, err := h.mcpHost.ListTools(r.Context(), agentID, serverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "mcp tools/list failed: "+err.Error())
		return
	}
	names := make([]string, 0, len(toolsList))
	for _, t := range toolsList {
		names = append(names, t.Name)
	}
	sort.Strings(names)

	writeJSON(w, http.StatusOK, map[string]any{
		"tool_count": len(names),
		"tools":      names,
	})
}
