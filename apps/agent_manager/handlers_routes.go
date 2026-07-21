package main

import (
	"net/http"
	"strings"
)

type agentCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type agentActionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute)

var agentCollectionHandlers = map[string]agentCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listAgents(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createAgent(w, r)
	},
}

var agentRootHandlers = map[string]agentActionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getAgent(w, r, route.agentID)
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.updateAgent(w, r, route.agentID)
	},
}

var agentActionHandlers = map[string]agentActionHandlerFunc{
	"clone": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.cloneAgent(w, r, route.agentID)
	},
	"start": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.startAgent(w, r, route.agentID)
	},
	"stop": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.stopAgent(w, r, route.agentID)
	},
	"restart": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.restartAgent(w, r, route.agentID)
	},
	"refresh-image": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.refreshAgentImage(w, r, route.agentID)
	},
	"logs": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getAgentLogs(w, r, route.agentID)
	},
	"recurring-tasks": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleRecurringTasks(w, r, route.agentID, route.taskID)
	},
	"capabilities": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleCapabilities(w, r, route.agentID, route.capID)
	},
	"terminal": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleTerminal(w, r, route.agentID)
	},
	"memory": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getMemory(w, r, route.agentID)
	},
	"archive": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getArchive(w, r, route.agentID)
	},
	"report": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getReport(w, r, route.agentID)
	},
	"soul": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getSoul(w, r, route.agentID)
	},
	"tasks": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getTasks(w, r, route.agentID)
	},
	"upload": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleUpload(w, r, route.agentID)
	},
	"chat-history": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getChatHistory(w, r, route.agentID)
	},
	"company": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getAgentCompany(w, r, route.agentID)
	},
	"runtime-status": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getRuntimeStatus(w, r, route.agentID)
	},
	"email-whitelist": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleEmailWhitelist(w, r, route.agentID)
	},
	"company-method-tools": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.getAgentCompanyMethodTools(w, r, route.agentID)
	},
	"mcp-servers": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.handleAgentMCPServers(w, r, route.agentID, route.serverID, route.serverAction)
	},
	"premium": func(h *Handlers, w http.ResponseWriter, r *http.Request, route agentRoute) {
		h.grantPremium(w, r, route.agentID)
	},
}

func isAgentCollectionMethod(method string) bool {
	_, ok := agentCollectionHandlers[method]
	return ok
}

func isAgentRootMethod(method string) bool {
	_, ok := agentRootHandlers[method]
	return ok
}

func isAgentAction(action string) bool {
	_, ok := agentActionHandlers[action]
	return ok
}

// handleIndex serves the main page.
// Also serves index.html for SPA routes so client-side routing works with bookmarks.
func (h *Handlers) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/missions") && !strings.HasPrefix(r.URL.Path, "/polymarket") {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read index.html")
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleHealth is the health check endpoint.
func (h *Handlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAgents handles GET /api/agents and POST /api/agents.
func (h *Handlers) handleAgents(w http.ResponseWriter, r *http.Request) {
	if !isAgentCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := agentCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r)
}

// handleAgent routes /api/agents/{id} and sub-paths.
func (h *Handlers) handleAgent(w http.ResponseWriter, r *http.Request) {
	route, ok := parseAgentRoute(r.URL.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "agent ID required")
		return
	}

	if h.handleAgentPrefixAction(w, r, route) {
		return
	}

	h.dispatchAgentAction(w, r, route)
}
func (h *Handlers) handleAgentPrefixAction(w http.ResponseWriter, r *http.Request, route agentRoute) bool {
	// Handle kg/* sub-paths
	if strings.HasPrefix(route.action, "kg") {
		h.handleKnowledgeGraph(w, r, route.agentID)
		return true
	}
	// Handle pending-emails/* sub-paths (approve/reject)
	if strings.HasPrefix(route.action, "pending-emails") {
		h.handlePendingEmails(w, r, route.agentID)
		return true
	}
	// Handle spend/* sub-paths
	if strings.HasPrefix(route.action, "spend") {
		h.handleSpend(w, r, route.agentID, route.action)
		return true
	}
	return false
}

func (h *Handlers) dispatchAgentAction(w http.ResponseWriter, r *http.Request, route agentRoute) {
	if route.action == "" {
		if !isAgentRootMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := agentRootHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, route)
		return
	}

	if !isAgentAction(route.action) {
		writeError(w, http.StatusNotFound, "unknown action: "+route.action)
		return
	}
	handler, ok := agentActionHandlers[route.action]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown action: "+route.action)
		return
	}
	handler(h, w, r, route)
}
