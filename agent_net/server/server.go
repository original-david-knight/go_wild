package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

// Config holds server configuration.
type Config struct {
	// Address to listen on (e.g., ":8080").
	Address string

	// Treasury addresses for blockchain verification.
	Treasury gowild_agent_net.TreasuryAddresses

	// SolanaRPCURL for blockchain verification.
	SolanaRPCURL string

	// BaseDifficulty for PoW (default: 20).
	BaseDifficulty int

	// ReadTimeout for HTTP server.
	ReadTimeout time.Duration

	// WriteTimeout for HTTP server.
	WriteTimeout time.Duration

	// A2ACallbackSigningKey is an optional base64url/base64/hex key used to sign A2A callbacks.
	A2ACallbackSigningKey string
}

// DefaultConfig returns default server configuration.
func DefaultConfig() Config {
	return Config{
		Address:        ":8080",
		BaseDifficulty: gowild_agent_net.DefaultBaseDifficulty,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}
}

// Server is the HTTP server for the agent network.
type Server struct {
	config    Config
	db        gowild_data.Database
	service   *gowild_agent_net.Service
	handlers  *Handlers
	paywall   *PaywallHandlers
	sites     *SiteHandlers
	wsHub     *WSHub
	a2aSigner *A2ACallbackSigner
	server    *http.Server
}

// NewServer creates a new agent network server.
func NewServer(db gowild_data.Database, config Config) *Server {
	// Create blockchain verifier if RPC URL provided
	var verifier *gowild_agent_net.BlockchainVerifier
	if config.SolanaRPCURL != "" {
		verifier = gowild_agent_net.NewBlockchainVerifier(config.SolanaRPCURL, config.Treasury)
	}

	service := gowild_agent_net.NewService(db, verifier)
	wsHub := NewWSHub()
	handlers := NewHandlers(service, config.Treasury, wsHub)
	paywall := NewPaywallHandlers(db)
	sites := NewSiteHandlers(db)

	return &Server{
		config:    config,
		db:        db,
		service:   service,
		handlers:  handlers,
		paywall:   paywall,
		sites:     sites,
		wsHub:     wsHub,
		a2aSigner: NewA2ACallbackSigner(config.A2ACallbackSigningKey),
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Addr:         s.config.Address,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	// Start cleanup workers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.service.StartCleanupWorker(ctx)

	// Start message cleanup worker (every 10 minutes)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if deleted, err := s.service.CleanupExpiredMessages(ctx); err == nil && deleted > 0 {
					// Cleanup ran successfully
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start A2A queue maintenance worker.
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = s.service.CleanupA2AQueue(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start A2A callback delivery worker.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.processA2ACallbacks(ctx, 25)
			case <-ctx.Done():
				return
			}
		}
	}()

	return s.server.ListenAndServe()
}

// handler returns the HTTP handler (useful for testing).
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return mux
}

// registerRoutes registers all API routes.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Frontpage - human-readable HTML view
	mux.Handle("/", PublicChain(http.HandlerFunc(s.handlers.HandleFrontpage)))

	// Agent accounts - human-readable list of all agents
	mux.Handle("/accounts", PublicChain(http.HandlerFunc(s.handlers.HandleAgentsList)))

	// Individual agent profile pages
	mux.Handle("/a/", PublicChain(http.HandlerFunc(s.handlers.HandleAgentProfile)))

	// Public endpoints (no auth)
	mux.Handle("/health", PublicChain(http.HandlerFunc(s.handlers.HandleHealth)))
	mux.Handle("/help", PublicChain(http.HandlerFunc(s.handlers.HandleHelp)))
	mux.Handle("/skill.md", PublicChain(http.HandlerFunc(s.handlers.HandleHelp)))
	mux.Handle("/a2a_skill.md", PublicChain(http.HandlerFunc(s.handlers.HandleA2ASkill)))
	mux.Handle("/api/v1/difficulty", PublicChain(http.HandlerFunc(s.handlers.HandleGetDifficulty)))
	mux.Handle("/api/v1/treasury", PublicChain(http.HandlerFunc(s.handlers.HandleGetTreasury)))
	mux.Handle("/api/v1/pow/test", PublicChain(http.HandlerFunc(s.handlers.HandlePoWTest)))

	// Public read endpoints
	mux.Handle("/api/v1/posts", s.postsHandler())
	mux.Handle("/api/v1/posts/", s.postsHandler())

	// Public profile read endpoint (matches /api/v1/profile/{publicKey})
	mux.Handle("/api/v1/profile/", PublicChain(http.HandlerFunc(s.handlers.HandleGetProfile)))

	// Authenticated endpoints (signature + PoW/Premium)
	mux.Handle("/api/v1/account/upgrade", SignatureOnlyChain(s.service, http.HandlerFunc(s.handlers.HandleUpgrade)))
	mux.Handle("/api/v1/account", SignatureOnlyChain(s.service, http.HandlerFunc(s.handlers.HandleDeleteAccount)))
	mux.Handle("/api/v1/profile", AuthChain(s.service, http.HandlerFunc(s.handlers.HandleUpdateProfile)))

	// Messaging endpoints (premium only)
	mux.Handle("/api/v1/messages/ws", PublicChain(http.HandlerFunc(s.handlers.HandleWSUpgrade)))
	mux.Handle("/api/v1/messages", s.messagesHandler())
	mux.Handle("/api/v1/messages/", s.messagesDetailHandler())

	// A2A async job endpoints (premium only)
	mux.Handle("/api/v1/a2a/jobs", s.a2aJobsHandler())
	mux.Handle("/api/v1/a2a/jobs/", s.a2aJobsDetailHandler())

	// Authenticated paywall endpoint (premium agents creating products)
	mux.Handle("/api/v1/paywall/create", PremiumAuthChainLargeBody(s.service, http.HandlerFunc(s.paywall.handleCreate)))

	// Public paywall endpoints (unauthenticated, customer-facing)
	mux.Handle("/paywall/dl/", PublicChain(http.HandlerFunc(s.paywall.HandlePaywallDownload)))
	mux.Handle("/paywall/", PublicChain(http.HandlerFunc(s.paywall.HandlePaywallRoute)))

	// Sites endpoints (premium agents publishing static websites)
	mux.Handle("/api/v1/sites/publish", PremiumAuthChainLargeBody(s.service, http.HandlerFunc(s.sites.handlePublish)))
	mux.Handle("/api/v1/sites/list", PremiumAuthChain(s.service, http.HandlerFunc(s.sites.handleList)))

	// Public static site serving
	mux.Handle("/sites/", PublicChain(http.HandlerFunc(s.sites.HandleServeStatic)))
}

// postsHandler handles both GET (public) and POST (authenticated) for /api/v1/posts.
func (s *Server) postsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a specific post request (e.g., /api/v1/posts/123)
		if postID, ok := pathAfterPrefix(r.URL.Path, "/api/v1/posts/"); ok && postID != "" {
			// Public endpoint for getting a specific post
			PublicChain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.handlers.HandleGetPost(w, r, postID)
			})).ServeHTTP(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			// Public endpoint for listing posts
			PublicChain(http.HandlerFunc(s.handlers.HandleListPosts)).ServeHTTP(w, r)
		case http.MethodPost:
			// Authenticated endpoint for creating posts
			AuthChain(s.service, http.HandlerFunc(s.handlers.HandleCreatePost)).ServeHTTP(w, r)
		default:
			writeBadRequest(w, "Method not allowed")
		}
	})
}

// messagesHandler handles both GET (list conversations) and POST (send message) for /api/v1/messages.
func (s *Server) messagesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			PremiumAuthChain(s.service, http.HandlerFunc(s.handlers.HandleListConversations)).ServeHTTP(w, r)
		case http.MethodPost:
			PremiumAuthChain(s.service, http.HandlerFunc(s.handlers.HandleSendMessage)).ServeHTTP(w, r)
		default:
			writeBadRequest(w, "Method not allowed")
		}
	})
}

// messagesDetailHandler handles /api/v1/messages/{id_or_pubkey}[/read] routes.
func (s *Server) messagesDetailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trimmed, ok := pathAfterPrefix(r.URL.Path, "/api/v1/messages/")
		if !ok {
			writeBadRequest(w, "Invalid messaging path")
			return
		}

		// Check for /api/v1/messages/{id}/read
		if strings.HasSuffix(trimmed, "/read") {
			messageID := strings.TrimSuffix(trimmed, "/read")
			if messageID != "" {
				PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.handlers.HandleMarkRead(w, r, messageID)
				})).ServeHTTP(w, r)
				return
			}
		}

		// No further slashes — could be a conversation pubkey or a message ID for delete
		if pathIsSingleSegment(trimmed) {
			switch r.Method {
			case http.MethodGet:
				// GET /api/v1/messages/{pubkey} — get conversation
				peerPubKey := trimmed
				PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.handlers.HandleGetConversation(w, r, peerPubKey)
				})).ServeHTTP(w, r)
			case http.MethodDelete:
				// DELETE /api/v1/messages/{id} — delete message
				messageID := trimmed
				PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.handlers.HandleDeleteMessage(w, r, messageID)
				})).ServeHTTP(w, r)
			default:
				writeBadRequest(w, "Method not allowed")
			}
			return
		}

		writeBadRequest(w, "Invalid messaging path")
	})
}

func (s *Server) a2aJobsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			PremiumAuthChain(s.service, http.HandlerFunc(s.handlers.HandleA2ASubmitJob)).ServeHTTP(w, r)
		default:
			writeBadRequest(w, "Method not allowed")
		}
	})
}

func (s *Server) a2aJobsDetailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trimmed, ok := pathAfterPrefix(r.URL.Path, "/api/v1/a2a/jobs/")
		if !ok || trimmed == "" {
			writeBadRequest(w, "Invalid A2A job path")
			return
		}

		if trimmed == "claim" {
			PremiumAuthChain(s.service, http.HandlerFunc(s.handlers.HandleA2AClaimJobs)).ServeHTTP(w, r)
			return
		}

		if strings.HasSuffix(trimmed, "/complete") {
			jobID := strings.TrimSuffix(trimmed, "/complete")
			if !pathIsSingleSegment(jobID) {
				writeBadRequest(w, "Invalid A2A complete path")
				return
			}
			PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.handlers.HandleA2ACompleteJob(w, r, jobID)
			})).ServeHTTP(w, r)
			return
		}

		if strings.HasSuffix(trimmed, "/heartbeat") {
			jobID := strings.TrimSuffix(trimmed, "/heartbeat")
			if !pathIsSingleSegment(jobID) {
				writeBadRequest(w, "Invalid A2A heartbeat path")
				return
			}
			PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.handlers.HandleA2AExtendLease(w, r, jobID)
			})).ServeHTTP(w, r)
			return
		}

		if pathIsSingleSegment(trimmed) {
			jobID := trimmed
			PremiumAuthChain(s.service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.handlers.HandleA2AGetJob(w, r, jobID)
			})).ServeHTTP(w, r)
			return
		}

		writeBadRequest(w, "Invalid A2A job path")
	})
}

func pathAfterPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}

func pathIsSingleSegment(path string) bool {
	return path != "" && !strings.Contains(path, "/")
}
