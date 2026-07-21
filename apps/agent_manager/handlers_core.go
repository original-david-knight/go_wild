package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

type spendActionHandler func(sh *SpendHandler, w http.ResponseWriter, r *http.Request, agentID string)

var spendActionHandlers = map[string]spendActionHandler{
	"spend": func(sh *SpendHandler, w http.ResponseWriter, r *http.Request, agentID string) {
		sh.getAgentSpend(w, r, agentID)
	},
	"spend/limits": func(sh *SpendHandler, w http.ResponseWriter, r *http.Request, agentID string) {
		sh.setSpendLimit(w, r, agentID)
	},
}

// Handlers holds the HTTP request handlers.
type Handlers struct {
	service          *AgentService
	docker           *dockermgr.DockerManager
	hub              *SessionHub
	brokerSecret     []byte
	workerManager    *WorkerManager
	mcpHost          *MCPHostManager
	pipelineEngine   *PipelineEngine
	spendHandler     *SpendHandler
	polymarketHelper *BrokerPolymarketHandler
	walletHelper     *BrokerWalletHandler

	companyPolymarketClientFactory func(context.Context, string) (companyPolymarketClient, error)
	companyWalletBalancesLoader    func(context.Context, string) (map[string]any, error)

	// jobDeliveryFunc delivers queued company method jobs to an agent.
	// Injected to decouple Handlers from BrokerToolsHandler.
	jobDeliveryFunc func(ctx context.Context, agentID string, batch int) (int, error)

	// shutdownCtx is a process-wide lifecycle context. Long-running HTTP
	// handlers that detach from the request lifecycle (currently the deep
	// research test and stream endpoints, which use detachedDeepResearchContext)
	// use it to observe SIGTERM so graceful shutdown doesn't have to wait on
	// per-method timeouts. Nil disables the signal, which is the default for
	// tests.
	shutdownCtx context.Context

	agentNetDBOnce sync.Once
	agentNetDB     gowild_data.Database
	agentNetDBErr  error
}

// NewHandlers creates a new handlers instance.
func NewHandlers(service *AgentService, docker *dockermgr.DockerManager, hub *SessionHub, workerManager *WorkerManager, mcpHost *MCPHostManager) *Handlers {
	h := &Handlers{
		service:       service,
		docker:        docker,
		hub:           hub,
		workerManager: workerManager,
		mcpHost:       mcpHost,
	}
	h.polymarketHelper = NewBrokerPolymarketHandler(service)
	h.walletHelper = NewBrokerWalletHandler(service)
	h.companyPolymarketClientFactory = h.newCompanyPolymarketClient
	h.companyWalletBalancesLoader = h.loadCompanyWalletBalances
	return h
}

// getAgentNetDB returns a lazy-initialized connection to the agent_net production database.
// Returns (nil, nil) if AGENT_NET_DATABASE_URL is not set.
// If agentNetDB was pre-set (e.g. in tests), it is returned directly.
func (h *Handlers) getAgentNetDB() (gowild_data.Database, error) {
	h.agentNetDBOnce.Do(func() {
		h.agentNetDB, h.agentNetDBErr = h.loadAgentNetDB()
	})
	return h.agentNetDB, h.agentNetDBErr
}

func (h *Handlers) loadAgentNetDB() (gowild_data.Database, error) {
	if h.agentNetDB != nil {
		return h.agentNetDB, nil // already injected (e.g. tests)
	}
	connStr := strings.TrimSpace(os.Getenv("AGENT_NET_DATABASE_URL"))
	if connStr == "" {
		return nil, nil // no agent-net DB configured
	}
	return openAgentNetDB(connStr)
}

func openAgentNetDB(connStr string) (gowild_data.Database, error) {
	db, err := gowild_data.NewPostgresDatabase(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent_net DB: %w", err)
	}
	if err := db.AddTable(gowild_agent_net.PremiumAgent{}); err != nil {
		return nil, fmt.Errorf("failed to register premium_agents table: %w", err)
	}
	return db, nil
}

// handleSpend dispatches spend sub-paths to the SpendHandler.
func (h *Handlers) handleSpend(w http.ResponseWriter, r *http.Request, agentID, action string) {
	if h.spendHandler == nil {
		writeError(w, http.StatusNotImplemented, "spend handler not configured")
		return
	}
	handler, ok := spendActionHandlers[action]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown spend action: "+action)
		return
	}
	handler(h.spendHandler, w, r, agentID)
}
