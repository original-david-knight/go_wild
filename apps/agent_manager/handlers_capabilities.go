package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
)

type capabilityCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string)
type capabilityHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, capID string)

var capabilityCollectionHandlers = map[string]capabilityCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.listCapabilities(w, r, agentID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.createCapability(w, r, agentID)
	},
}

var capabilityHandlers = map[string]capabilityHandlerFunc{
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, capID string) {
		h.deleteCapability(w, r, agentID, capID)
	},
}

func isCapabilityCollectionMethod(method string) bool {
	_, ok := capabilityCollectionHandlers[method]
	return ok
}

func isCapabilityMethod(method string) bool {
	_, ok := capabilityHandlers[method]
	return ok
}

func (h *Handlers) handleCapabilities(w http.ResponseWriter, r *http.Request, agentID, capID string) {
	// Operations on a specific capability
	if capID != "" {
		if !isCapabilityMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := capabilityHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, agentID, capID)
		return
	}

	// Operations on the collection
	if !isCapabilityCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := capabilityCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, agentID)
}

func (h *Handlers) listCapabilities(w http.ResponseWriter, r *http.Request, agentID string) {
	caps, err := h.service.GetCapabilities(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get capabilities: "+err.Error())
		return
	}

	methods, err := h.service.ListA2AMethods(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get methods: "+err.Error())
		return
	}
	methodByName := make(map[string]data.A2AMethod, len(methods))
	for _, m := range methods {
		methodByName[strings.TrimSpace(m.Method)] = m
	}

	capList := make([]map[string]any, len(caps))
	for i, c := range caps {
		desc := ""
		instructions := ""
		var inputSchema any
		var outputSchema any
		if m, ok := methodByName[strings.TrimSpace(c.Method)]; ok {
			desc = strings.TrimSpace(m.Description)
			instructions = strings.TrimSpace(m.Instructions)
			if parsed, err := parseCapabilitySchema(m.InputSchemaJSON); err == nil && parsed != nil {
				inputSchema = parsed
			}
			if parsed, err := parseCapabilitySchema(m.OutputSchemaJSON); err == nil && parsed != nil {
				outputSchema = parsed
			}
		}

		item := map[string]any{
			"id":            c.ID,
			"role":          c.Role,
			"method":        c.Method,
			"description":   desc,
			"instructions":  instructions,
			"registered_at": c.RegisteredAt.Format("2006-01-02T15:04:05Z07:00"),
		}

		if inputSchema != nil {
			item["input_schema"] = inputSchema
		}
		if outputSchema != nil {
			item["output_schema"] = outputSchema
		}

		capList[i] = item
	}

	writeJSON(w, http.StatusOK, map[string]any{"capabilities": capList})
}

type CreateCapabilityRequest struct {
	Role   string `json:"role"`
	Method string `json:"method"`
}

func (h *Handlers) createCapability(w http.ResponseWriter, r *http.Request, agentID string) {
	var req CreateCapabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	if req.Method == "" {
		writeError(w, http.StatusBadRequest, "method is required")
		return
	}

	cap, err := h.service.AddCapability(r.Context(), agentID, req.Role, req.Method)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "unknown method") || strings.Contains(err.Error(), "already exists") {
			status = http.StatusBadRequest
			if strings.Contains(err.Error(), "already exists") {
				status = http.StatusConflict
			}
		}
		writeError(w, status, "failed to create capability: "+err.Error())
		return
	}

	// Join method metadata for the response (description + schemas).
	method, _ := h.service.GetA2AMethod(r.Context(), req.Method)
	desc := ""
	instructions := ""
	var inputSchema any
	var outputSchema any
	if method != nil {
		desc = strings.TrimSpace(method.Description)
		instructions = strings.TrimSpace(method.Instructions)
		if parsed, err := parseCapabilitySchema(method.InputSchemaJSON); err == nil && parsed != nil {
			inputSchema = parsed
		}
		if parsed, err := parseCapabilitySchema(method.OutputSchemaJSON); err == nil && parsed != nil {
			outputSchema = parsed
		}
	}

	resp := map[string]any{
		"id":            cap.ID,
		"role":          cap.Role,
		"method":        cap.Method,
		"description":   desc,
		"instructions":  instructions,
		"registered_at": cap.RegisteredAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if inputSchema != nil {
		resp["input_schema"] = inputSchema
	}
	if outputSchema != nil {
		resp["output_schema"] = outputSchema
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) deleteCapability(w http.ResponseWriter, r *http.Request, agentID, capID string) {
	if err := h.service.DeleteCapability(r.Context(), agentID, capID); err != nil {
		writeError(w, http.StatusNotFound, "capability not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
