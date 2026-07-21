package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

var (
	methodTokenRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)
)

type a2aMethodRequest struct {
	Method                            string          `json:"method"`
	Description                       string          `json:"description"`
	Instructions                      string          `json:"instructions"`
	InputSchema                       json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema                      json.RawMessage `json:"output_schema,omitempty"`
	ModelTier                         *string         `json:"model_tier,omitempty"`
	AutoMarketNote                    *bool           `json:"auto_market_note,omitempty"`
	FreshContext                      *bool           `json:"fresh_context,omitempty"`
	RedactMarketPrices                *bool           `json:"redact_market_prices,omitempty"`
	DisableMarketNotes                *bool           `json:"disable_market_notes,omitempty"`
	DisablePolymarketNoteAugmentation *bool           `json:"disable_polymarket_note_augmentation,omitempty"`
	DisabledToolGroups                []string        `json:"disabled_tool_groups,omitempty"`
	CompletionTimestampKey *string `json:"completion_timestamp_key,omitempty"`
	CompletionSuccessKey  *string `json:"completion_success_key,omitempty"`
}

type a2aMethodCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type a2aMethodHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, method string)

var a2aMethodCollectionHandlers = map[string]a2aMethodCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listA2AMethods(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createA2AMethod(w, r)
	},
}

var a2aMethodHandlers = map[string]a2aMethodHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.getA2AMethod(w, r, method)
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.updateA2AMethod(w, r, method)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, method string) {
		h.deleteA2AMethod(w, r, method)
	},
}

func isA2AMethodCollectionMethod(method string) bool {
	_, ok := a2aMethodCollectionHandlers[method]
	return ok
}

func isA2AMethodMethod(method string) bool {
	_, ok := a2aMethodHandlers[method]
	return ok
}

func parseA2AMethodRoute(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/a2a-methods")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func (h *Handlers) handleA2AMethods(w http.ResponseWriter, r *http.Request) {
	method := parseA2AMethodRoute(r.URL.Path)

	// Collection routes.
	if method == "" {
		if !isA2AMethodCollectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := a2aMethodCollectionHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r)
		return
	}

	// Single-method routes.
	if !isA2AMethodMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := a2aMethodHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, method)
}

func (h *Handlers) listA2AMethods(w http.ResponseWriter, r *http.Request) {
	methods, err := h.service.ListA2AMethods(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list methods: "+err.Error())
		return
	}

	out := make([]map[string]any, len(methods))
	for i, m := range methods {
		out[i] = a2aMethodResponseMap(m)
	}

	writeJSON(w, http.StatusOK, map[string]any{"methods": out})
}

func (h *Handlers) getA2AMethod(w http.ResponseWriter, r *http.Request, method string) {
	m, err := h.service.GetA2AMethod(r.Context(), method)
	if err != nil {
		writeError(w, http.StatusNotFound, "method not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, a2aMethodResponseMap(*m))
}

func (h *Handlers) createA2AMethod(w http.ResponseWriter, r *http.Request) {
	var req a2aMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	method := strings.TrimSpace(req.Method)
	if method == "" {
		writeError(w, http.StatusBadRequest, "method is required")
		return
	}
	if !methodTokenRe.MatchString(method) {
		writeError(w, http.StatusBadRequest, "method contains invalid characters (use a simple token like fulfill_order)")
		return
	}

	inputSchemaJSON, err := normalizeCapabilitySchema(req.InputSchema, "input_schema")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	outputSchemaJSON, err := normalizeCapabilitySchema(req.OutputSchema, "output_schema")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	autoMarketNote := req.AutoMarketNote != nil && *req.AutoMarketNote
	freshContext := req.FreshContext != nil && *req.FreshContext
	redactMarketPrices := req.RedactMarketPrices != nil && *req.RedactMarketPrices
	disableMarketNotes := req.DisableMarketNotes != nil && *req.DisableMarketNotes
	disablePolymarketNoteAugmentation := req.DisablePolymarketNoteAugmentation != nil && *req.DisablePolymarketNoteAugmentation
	var methodOpts []data.A2AMethodOption
	if req.ModelTier != nil {
		methodOpts = append(methodOpts, data.WithModelTier(*req.ModelTier))
	}
	if len(req.DisabledToolGroups) > 0 {
		methodOpts = append(methodOpts, data.WithDisabledToolGroups(req.DisabledToolGroups))
	}
	if req.CompletionTimestampKey != nil || req.CompletionSuccessKey != nil {
		tsKey := ""
		okKey := ""
		if req.CompletionTimestampKey != nil {
			tsKey = *req.CompletionTimestampKey
		}
		if req.CompletionSuccessKey != nil {
			okKey = *req.CompletionSuccessKey
		}
		methodOpts = append(methodOpts, data.WithCompletionKeys(tsKey, okKey))
	}
	m, err := h.service.CreateA2AMethodWithConfig(r.Context(), method, req.Description, req.Instructions, inputSchemaJSON, outputSchemaJSON, autoMarketNote, freshContext, redactMarketPrices, disableMarketNotes, disablePolymarketNoteAugmentation, methodOpts...)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeError(w, status, "failed to create method: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, a2aMethodResponseMap(*m))
}

func (h *Handlers) updateA2AMethod(w http.ResponseWriter, r *http.Request, method string) {
	if strings.TrimSpace(method) == "" {
		writeError(w, http.StatusBadRequest, "method is required")
		return
	}

	existing, err := h.service.GetA2AMethod(r.Context(), method)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "method not found")
		return
	}

	var req a2aMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	inputSchemaJSON := existing.InputSchemaJSON
	outputSchemaJSON := existing.OutputSchemaJSON
	if req.InputSchema != nil {
		normalized, err := normalizeCapabilitySchema(req.InputSchema, "input_schema")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		inputSchemaJSON = normalized
	}
	if req.OutputSchema != nil {
		normalized, err := normalizeCapabilitySchema(req.OutputSchema, "output_schema")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		outputSchemaJSON = normalized
	}

	autoMarketNote := existing.AutoMarketNote
	if req.AutoMarketNote != nil {
		autoMarketNote = *req.AutoMarketNote
	}
	freshContext := existing.FreshContext
	if req.FreshContext != nil {
		freshContext = *req.FreshContext
	}
	redactMarketPrices := existing.RedactMarketPrices
	if req.RedactMarketPrices != nil {
		redactMarketPrices = *req.RedactMarketPrices
	}
	disableMarketNotes := existing.DisableMarketNotes
	if req.DisableMarketNotes != nil {
		disableMarketNotes = *req.DisableMarketNotes
	}
	disablePolymarketNoteAugmentation := existing.DisablePolymarketNoteAugmentation
	if req.DisablePolymarketNoteAugmentation != nil {
		disablePolymarketNoteAugmentation = *req.DisablePolymarketNoteAugmentation
	}
	var methodOpts []data.A2AMethodOption
	if req.ModelTier != nil {
		methodOpts = append(methodOpts, data.WithModelTier(*req.ModelTier))
	}
	if req.DisabledToolGroups != nil {
		methodOpts = append(methodOpts, data.WithDisabledToolGroups(req.DisabledToolGroups))
	}
	{
		tsKey := existing.CompletionTimestampKey
		okKey := existing.CompletionSuccessKey
		changed := false
		if req.CompletionTimestampKey != nil {
			tsKey = *req.CompletionTimestampKey
			changed = true
		}
		if req.CompletionSuccessKey != nil {
			okKey = *req.CompletionSuccessKey
			changed = true
		}
		if changed {
			methodOpts = append(methodOpts, data.WithCompletionKeys(tsKey, okKey))
		}
	}
	m, err := h.service.UpdateA2AMethodWithConfig(r.Context(), method, req.Description, req.Instructions, inputSchemaJSON, outputSchemaJSON, autoMarketNote, freshContext, redactMarketPrices, disableMarketNotes, disablePolymarketNoteAugmentation, methodOpts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update method: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, a2aMethodResponseMap(*m))
}

func (h *Handlers) deleteA2AMethod(w http.ResponseWriter, r *http.Request, method string) {
	// Refuse deletion if any agent capability references this method.
	capDAO := h.service.db.Table(data.AgentCapability{})
	results, err := capDAO.Query(r.Context(), gowild_data.QueryOpts{
		Where: map[string]any{"method": method},
		Limit: 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check method usage: "+err.Error())
		return
	}
	if len(results) > 0 {
		writeError(w, http.StatusConflict, "method is in use by one or more agent capabilities")
		return
	}

	if err := h.service.DeleteA2AMethod(r.Context(), method); err != nil {
		writeError(w, http.StatusNotFound, "method not found: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func a2aMethodResponseMap(m data.A2AMethod) map[string]any {
	resp := map[string]any{
		"method":                               m.Method,
		"description":                          m.Description,
		"instructions":                         m.Instructions,
		"model_tier":                           m.ModelTier,
		"auto_market_note":                     m.AutoMarketNote,
		"fresh_context":                        m.FreshContext,
		"redact_market_prices":                 m.RedactMarketPrices,
		"disable_market_notes":                 m.DisableMarketNotes,
		"disable_polymarket_note_augmentation": m.DisablePolymarketNoteAugmentation,
		"disabled_tool_groups":                 m.DisabledToolGroups(),
		"completion_timestamp_key":             m.CompletionTimestampKey,
		"completion_success_key":               m.CompletionSuccessKey,
		"created_at":                           m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_at":                           m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if inputSchema, err := parseCapabilitySchema(m.InputSchemaJSON); err == nil && inputSchema != nil {
		resp["input_schema"] = inputSchema
	}
	if outputSchema, err := parseCapabilitySchema(m.OutputSchemaJSON); err == nil && outputSchema != nil {
		resp["output_schema"] = outputSchema
	}
	return resp
}
