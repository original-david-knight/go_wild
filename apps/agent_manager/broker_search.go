package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/original-david-knight/go_wild/my"
	"github.com/original-david-knight/go_wild/tools"
)

// BrokerSearchHandler handles search proxy requests.
type BrokerSearchHandler struct {
	apiKey string
}

// NewBrokerSearchHandler creates a new search broker handler.
func NewBrokerSearchHandler() *BrokerSearchHandler {
	gowild_my.LoadEnv()
	return &BrokerSearchHandler{
		apiKey: strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
	}
}

func (h *BrokerSearchHandler) credential() string {
	if strings.TrimSpace(h.apiKey) == "" {
		gowild_my.LoadEnv()
		if strings.TrimSpace(h.apiKey) == "" {
			h.apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
		}
	}
	return strings.TrimSpace(h.apiKey)
}

func (h *BrokerSearchHandler) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	apiKey := h.credential()
	if apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Gemini API not configured on broker (set GEMINI_API_KEY)")
		return
	}

	var input tools.WebSearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	webTools := tools.NewWebTools(apiKey)
	result, err := webTools.WebSearchTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result.ToMap())
}
