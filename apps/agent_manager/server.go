package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	brokerprotocol "github.com/original-david-knight/go_wild/tools/broker"
)

// BrokerHandlers groups all broker endpoint handlers.
type BrokerHandlers struct {
	auth       *BrokerAuthHandler
	llm        *BrokerLLMHandler
	wallet     *BrokerWalletHandler
	polymarket *BrokerPolymarketHandler
	email      *BrokerEmailHandler
	search     *BrokerSearchHandler
	telegram   *BrokerTelegramHandler
	tools      *BrokerToolsHandler
	paywall    *BrokerPaywallHandler
	sites      *BrokerSitesHandler
	secret     []byte
}

// Server is the HTTP server for the agent manager.
type Server struct {
	addr          string
	handler       *Handlers
	broker        *BrokerHandlers
	webhookRouter *WebhookRouter
}

// NewServer creates a new HTTP server.
func NewServer(addr string, handler *Handlers, broker *BrokerHandlers, webhookRouter *WebhookRouter) *Server {
	return &Server{addr: addr, handler: handler, broker: broker, webhookRouter: webhookRouter}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	handler := s.buildHandler()
	server := &http.Server{
		Addr:         s.addr,
		Handler:      handler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 5 * time.Minute, // LLM generation can take minutes
		IdleTimeout:  120 * time.Second,
	}

	fmt.Printf("Agent Manager listening on %s\n", s.addr)
	return server.ListenAndServe()
}

func (s *Server) buildHandler() http.Handler {
	mux := s.buildMux()
	return chain(mux, loggingMiddleware, recoveryMiddleware, corsMiddleware)
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	registerStaticRoutes(mux, s.handler)
	registerManagerAPIRoutes(mux, s.handler)

	// Broker API endpoints. Auth initiation is public; all other broker calls
	// require a session token from the Ethereum challenge-response flow.
	if s.broker != nil {
		brokerMux := buildBrokerMux(s.broker)
		authMux := buildBrokerAuthMux(s.broker)

		mux.Handle("/broker/v1/auth/", authMux)
		mux.Handle("/broker/", brokerSessionAuthMiddleware(s.broker.auth, brokerMux))
	}

	return mux
}

func registerStaticRoutes(mux *http.ServeMux, handler *Handlers) {
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticSubFS)))
	mux.Handle("/static/", withNoCache(staticHandler))
	mux.HandleFunc("/", handler.handleIndex)
}

func registerManagerAPIRoutes(mux *http.ServeMux, handler *Handlers) {
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/health", handler: handler.handleHealth},
		{pattern: "/api/agents", handler: handler.handleAgents},
		{pattern: "/api/agents/", handler: handler.handleAgent},
		{pattern: "/api/peer-groups", handler: handler.handlePeerGroups},
		{pattern: "/api/peer-groups/", handler: handler.handlePeerGroup},
		{pattern: "/api/companies", handler: handler.handleCompanies},
		{pattern: "/api/companies/", handler: handler.handleCompany},
		{pattern: "/api/public-endpoints", handler: handler.handlePublicEndpoints},
		{pattern: "/api/mcp-servers", handler: handler.handleMCPServers},
		{pattern: "/api/mcp-servers/", handler: handler.handleMCPServer},
		{pattern: "/api/docker/build", handler: handler.handleDockerBuild},
		{pattern: "/api/docker/status", handler: handler.handleDockerStatus},
		{pattern: "/api/a2a-methods", handler: handler.handleA2AMethods},
		{pattern: "/api/a2a-methods/", handler: handler.handleA2AMethods},
		{pattern: "/api/tool-groups", handler: handler.handleToolGroups},
		{pattern: "/api/deep-research-methods", handler: handler.handleDeepResearchMethods},
		{pattern: "/api/deep-research-methods/", handler: handler.handleDeepResearchMethods},
		{pattern: "/api/pipelines", handler: handler.handlePipelines},
		{pattern: "/api/pipelines/", handler: handler.handlePipelines},
		{pattern: "/api/pipelines/runs", handler: handler.handlePipelineRuns},
		{pattern: "/api/pipelines/runs/", handler: handler.handlePipelineRuns},
		{pattern: "/api/pipelines/jobs", handler: handler.handlePipelineJobs},
		{pattern: "/api/pipelines/jobs/", handler: handler.handlePipelineJobs},
		{pattern: "/api/builtin-methods/terminal", handler: handler.handleBuiltinMethodsTerminal},
	})
}

func withNoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

func buildBrokerMux(broker *BrokerHandlers) *http.ServeMux {
	brokerMux := http.NewServeMux()
	if broker == nil {
		return brokerMux
	}

	registerLLMBrokerRoutes(brokerMux, broker.llm)
	registerWalletBrokerRoutes(brokerMux, broker.wallet)
	registerPolymarketBrokerRoutes(brokerMux, broker.polymarket)
	registerEmailBrokerRoutes(brokerMux, broker.email)
	registerSearchBrokerRoutes(brokerMux, broker.search)
	registerTelegramBrokerRoutes(brokerMux, broker.telegram)
	registerToolsBrokerRoutes(brokerMux, broker.tools)
	registerPaywallBrokerRoutes(brokerMux, broker.paywall)
	registerSitesBrokerRoutes(brokerMux, broker.sites)

	return brokerMux
}

func buildBrokerAuthMux(broker *BrokerHandlers) *http.ServeMux {
	authMux := http.NewServeMux()
	if broker == nil || broker.auth == nil {
		return authMux
	}
	registerMuxRoutes(authMux, []muxRoute{
		{pattern: brokerprotocol.AuthChallengePath, handler: broker.auth.handleChallenge},
		{pattern: brokerprotocol.AuthVerifyPath, handler: broker.auth.handleVerify},
	})
	return authMux
}

func buildBrokerSocketMux(broker *BrokerHandlers) *http.ServeMux {
	socketMux := http.NewServeMux()
	if broker == nil {
		return socketMux
	}
	if broker.auth != nil {
		registerMuxRoutes(socketMux, []muxRoute{
			{pattern: brokerprotocol.AuthChallengePath, handler: broker.auth.handleChallenge},
			{pattern: brokerprotocol.AuthVerifyPath, handler: broker.auth.handleVerify},
		})
	}
	socketMux.Handle("/broker/", buildBrokerMux(broker))
	return socketMux
}

type muxRoute struct {
	pattern string
	handler http.HandlerFunc
}

func registerMuxRoutes(mux *http.ServeMux, routes []muxRoute) {
	for _, route := range routes {
		mux.HandleFunc(route.pattern, route.handler)
	}
}

func registerLLMBrokerRoutes(mux *http.ServeMux, llm *BrokerLLMHandler) {
	if llm == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/llm/generate", handler: llm.handleGenerate},
	})
}

func registerWalletBrokerRoutes(mux *http.ServeMux, wallet *BrokerWalletHandler) {
	if wallet == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/wallet/address", handler: wallet.handleGetAddress},
		{pattern: "/broker/v1/wallet/balance", handler: wallet.handleGetBalance},
		{pattern: "/broker/v1/wallet/balances", handler: wallet.handleGetBalances},
		{pattern: "/broker/v1/wallet/sign", handler: wallet.handleSign},
		{pattern: "/broker/v1/wallet/send", handler: wallet.handleSend},
		{pattern: "/broker/v1/wallet/swap", handler: wallet.handleSwap},
		{pattern: "/broker/v1/wallet/contract", handler: wallet.handleContract},
		{pattern: "/broker/v1/wallet/history", handler: wallet.handleHistory},
		{pattern: "/broker/v1/wallet/encrypt", handler: wallet.handleEncrypt},
		{pattern: "/broker/v1/wallet/decrypt", handler: wallet.handleDecrypt},
		{pattern: "/broker/v1/wallet/pubkey", handler: wallet.handlePubKey},
	})
}

func registerPolymarketBrokerRoutes(mux *http.ServeMux, polymarket *BrokerPolymarketHandler) {
	if polymarket == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/polymarket/search", handler: polymarket.handleSearchMarkets},
		{pattern: "/broker/v1/polymarket/market", handler: polymarket.handleGetMarket},
		{pattern: "/broker/v1/polymarket/prices", handler: polymarket.handleGetPrices},
		{pattern: "/broker/v1/polymarket/price-history", handler: polymarket.handleGetPriceHistory},
		{pattern: "/broker/v1/polymarket/candles", handler: polymarket.handleGetCandles},
		{pattern: "/broker/v1/polymarket/orderbook", handler: polymarket.handleGetOrderbook},
		{pattern: "/broker/v1/polymarket/orderbook-depth", handler: polymarket.handleOrderBookDepth},
		{pattern: "/broker/v1/polymarket/positions", handler: polymarket.handleGetPositions},
		{pattern: "/broker/v1/polymarket/order", handler: polymarket.handlePlaceOrder},
		{pattern: "/broker/v1/polymarket/cancel", handler: polymarket.handleCancelOrder},
		{pattern: "/broker/v1/polymarket/orders", handler: polymarket.handleGetOrders},
		{pattern: "/broker/v1/polymarket/trades", handler: polymarket.handleGetTrades},
		{pattern: "/broker/v1/polymarket/redeem", handler: polymarket.handleRedeemWinnings},
		{pattern: "/broker/v1/polymarket/market-notes/add", handler: polymarket.handleAddMarketNote},
		{pattern: "/broker/v1/polymarket/market-notes/list", handler: polymarket.handleListMarketNotes},
	})
}

func registerEmailBrokerRoutes(mux *http.ServeMux, email *BrokerEmailHandler) {
	if email == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/email/list", handler: email.handleList},
		{pattern: "/broker/v1/email/read", handler: email.handleRead},
		{pattern: "/broker/v1/email/send", handler: email.handleSend},
	})
}

func registerSearchBrokerRoutes(mux *http.ServeMux, search *BrokerSearchHandler) {
	if search == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/search/web", handler: search.handleWebSearch},
	})
}

func registerTelegramBrokerRoutes(mux *http.ServeMux, telegram *BrokerTelegramHandler) {
	if telegram == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/telegram/send", handler: telegram.handleSend},
		{pattern: "/broker/v1/telegram/updates", handler: telegram.handleGetUpdates},
		{pattern: "/broker/v1/telegram/chats", handler: telegram.handleGetChats},
		{pattern: "/broker/v1/telegram/bot_info", handler: telegram.handleBotInfo},
	})
}

func registerToolsBrokerRoutes(mux *http.ServeMux, tools *BrokerToolsHandler) {
	if tools == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/tools/", handler: tools.Handle},
		{pattern: "/broker/v1/mcp-tools/list", handler: tools.HandleListAgentTools},
	})
}

func registerPaywallBrokerRoutes(mux *http.ServeMux, paywall *BrokerPaywallHandler) {
	if paywall == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/paywall/create", handler: paywall.handlePaywallCreate},
	})
}

func registerSitesBrokerRoutes(mux *http.ServeMux, sites *BrokerSitesHandler) {
	if sites == nil {
		return
	}
	registerMuxRoutes(mux, []muxRoute{
		{pattern: "/broker/v1/sites/publish", handler: sites.handleSitePublish},
		{pattern: "/broker/v1/sites/list", handler: sites.handleSiteList},
	})
}

// Middleware

func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !isQuietRoute(r.Method, r.URL.Path) {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
		}
	})
}

func isQuietRoute(method, path string) bool {
	if strings.HasPrefix(path, "/static/") {
		return true
	}
	if method != "GET" {
		return false
	}
	switch {
	case path == "/api/agents":
		return true
	case path == "/api/pipelines/runs",
		path == "/api/pipelines/jobs":
		return true
	case strings.HasPrefix(path, "/api/pipelines/runs/"):
		return true
	case path == "/health":
		return true
	default:
		return false
	}
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v\n%s", err, debug.Stack())
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Gowild-Execution-Method")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Response helpers

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
