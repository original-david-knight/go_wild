package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"

	"google.golang.org/genai"
	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// BrokerToolsHandler handles the generic tool proxy endpoint.
// POST /broker/v1/tools/{tool_name}
type BrokerToolsHandler struct {
	db                              gowild_data.Database
	workerManager                   *WorkerManager
	sendHeartbeatFn                 func(agentID, message string) error
	genaiClient                     *genai.Client
	mcpHost                         *MCPHostManager
	spendGovernor                   *SpendGovernor
	pipelineEngine                  *PipelineEngine
	resolveClaudeVolumeMountpointFn func(ctx context.Context, agentID string) (string, error)
	// shutdownCtx is a process-wide lifecycle context. Long-running tool
	// calls that intentionally detach from the HTTP request lifecycle
	// (currently deep-research methods, see callDeepResearchMethodTools)
	// use it to observe SIGTERM so graceful shutdown doesn't have to wait
	// on per-method timeouts. Nil disables the signal, which is the
	// default for tests.
	shutdownCtx context.Context
}

// NewBrokerToolsHandler creates a new tools handler.
func NewBrokerToolsHandler(db gowild_data.Database) *BrokerToolsHandler {
	client, err := genai.NewClient(context.Background(), nil)
	if err != nil {
		log.Printf("WARNING: failed to create genai client: %v (compress_content will be unavailable)", err)
	}

	h := &BrokerToolsHandler{
		db:                              db,
		genaiClient:                     client,
		mcpHost:                         NewMCPHostManager(db),
		spendGovernor:                   NewSpendGovernor(db),
		resolveClaudeVolumeMountpointFn: resolveAgentVolumeMountpoint,
	}

	return h
}

// Handle routes a tool call to the correct implementation.
func (h *BrokerToolsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Extract tool name from path: /broker/v1/tools/{tool_name}
	path := r.URL.Path
	toolName := strings.TrimPrefix(path, "/broker/v1/tools/")
	if toolName == "" || toolName == path {
		writeError(w, http.StatusBadRequest, "missing tool name")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "no agent ID in context")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Create agent-scoped service
	if h.db == nil {
		writeError(w, http.StatusInternalServerError, "database connection is nil")
		return
	}
	agentService := data.NewAgentService(h.db, agentID)

	// Route to the correct tool
	result, toolErr := h.callTool(r.Context(), agentID, agentService, toolName, body)
	if toolErr != nil {
		writeError(w, http.StatusInternalServerError, toolErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
