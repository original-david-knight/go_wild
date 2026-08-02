package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/my"
	obj "github.com/original-david-knight/go_wild/objectives"
	objplan "github.com/original-david-knight/go_wild/objectives_planner"
)

const defaultAgentDBURL = "postgres://gowild_agent:gowild_agent@localhost:5432/gowild_agent"

func newGeminiClientFromEnv() (*loop.GeminiClient, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	return loop.NewGeminiClient(context.Background(), apiKey, loop.DefaultModel)
}

func main() {
	// Load .env from current or parent directories
	if envPath := gowild_my.LoadEnv(); envPath != "" {
		log.Printf("Loaded env: %s", envPath)
	}

	addr := flag.String("addr", "127.0.0.1:8888", "Manager API/UI listen address (keep private)")
	ingressAddr := flag.String("ingress-addr", "127.0.0.1:8890", "Ingress-only listen address for webhooks")
	agentDBURL := flag.String("agent-db", "", "PostgreSQL URL for agent database (default: GOWILD_DATABASE_URL env or postgres://gowild_agent:gowild_agent@localhost:5432/gowild_agent)")
	flag.Parse()

	// Resolve agent database URL
	dbURL := resolveAgentDBURL(*agentDBURL)

	// Connect to PostgreSQL database
	db, err := gowild_data.NewPostgresDatabase(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database (%s): %v", dbURL, err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		log.Fatalf("Failed to initialize manager schema: %v", err)
	}
	log.Printf("Connected to database: %s", dbURL)

	// Initialize Docker manager
	docker, err := dockermgr.NewDockerManager()
	if err != nil {
		log.Fatalf("Failed to initialize Docker: %v", err)
	}
	defer docker.Close()

	// Initialize services
	service := NewAgentService(db)
	hub := NewSessionHub(docker, service)

	// Initialize broker
	brokerSecret := loadOrGenerateBrokerSecret(db)
	geminiClient, err := newGeminiClientFromEnv()
	if err != nil {
		log.Printf("Warning: Gemini-only features disabled (no GEMINI_API_KEY): %v", err)
	}

	broker := &BrokerHandlers{
		auth:       NewBrokerAuthHandler(service, brokerSecret),
		wallet:     NewBrokerWalletHandler(service),
		polymarket: NewBrokerPolymarketHandler(service),
		email:      NewBrokerEmailHandler(service),
		search:     NewBrokerSearchHandler(),
		telegram:   NewBrokerTelegramHandler(service),
		tools:      NewBrokerToolsHandler(db),
		paywall:    NewBrokerPaywallHandler(db, docker),
		sites:      NewBrokerSitesHandler(db, docker),
		secret:     brokerSecret,
		llm:        NewBrokerLLMHandler(service),
	}
	log.Println("Broker service initialized")

	// Start local broker socket server for container-to-manager RPC without HTTP/TCP configuration.
	brokerSocket := NewBrokerSocketServer(brokerSocketPath(), broker)
	defer brokerSocket.Close()
	go func() {
		if err := brokerSocket.ListenAndServe(); err != nil {
			log.Fatalf("Broker socket server failed: %v", err)
		}
	}()
	if err := waitForBrokerSocket(brokerSocketPath(), 2*time.Second); err != nil {
		log.Fatalf("Broker socket startup failed: %v", err)
	}

	// Initialize worker manager for background tasks (Telegram monitoring, etc.)
	workerManager := NewWorkerManager(hub, service, broker.telegram, db)
	broker.telegram.workerManager = workerManager
	broker.tools.workerManager = workerManager

	// Create handlers (needs workerManager for lifecycle hooks)
	handlers := NewHandlers(service, docker, hub, workerManager, broker.tools.mcpHost)
	handlers.brokerSecret = brokerSecret
	handlers.jobDeliveryFunc = broker.tools.deliverQueuedCompanyMethodJobs

	// Manager-wide lifecycle context. Cancelled on SIGTERM so long-running
	// broker tool calls that detach from the HTTP request lifecycle (deep
	// research) still observe graceful shutdown instead of blocking on their
	// per-method timeout.
	managerCtx, managerCancel := context.WithCancel(context.Background())
	broker.tools.shutdownCtx = managerCtx
	handlers.shutdownCtx = managerCtx

	// Initialize pipeline engine
	pipelineEngine := NewPipelineEngine(db, service)
	pipelineEngine.SetHeartbeatSender(workerManager)
	pipelineEngine.CleanupStaleRuns(context.Background())
	handlers.pipelineEngine = pipelineEngine
	broker.tools.pipelineEngine = pipelineEngine
	pipelineCtx, pipelineCancel := context.WithCancel(context.Background())
	go pipelineEngine.Run(pipelineCtx)

	// Initialize objectives scheduler (uses same database)
	objSchedulerCtx, objSchedulerCancel := context.WithCancel(context.Background())
	objScheduler := startObjectivesScheduler(objSchedulerCtx, db, geminiClient)

	// Auto-start agents
	ctx := context.Background()
	autoStartAgents(ctx, service, docker, brokerSecret)

	// Start workers for auto-started agents
	startWorkersForRunningAgents(ctx, service, docker, workerManager)

	// Initialize webhook router and spend handler
	webhookRouter := NewWebhookRouter(db)
	handlers.spendHandler = NewSpendHandler(db)

	// Start webhook event processor
	webhookCtx, webhookCancel := context.WithCancel(context.Background())
	go webhookRouter.ProcessPendingEvents(webhookCtx)

	startHTTPServers(*addr, *ingressAddr, handlers, broker, webhookRouter)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	// Cancel the manager-wide lifecycle context first so in-flight deep
	// research runs (which use context.WithoutCancel to survive request
	// cancellation) observe shutdown and abort rather than running to
	// their per-method timeout.
	managerCancel()
	webhookCancel()
	pipelineCancel()
	objSchedulerCancel()
	if objScheduler != nil {
		objScheduler.Stop()
	}
	// Cancel every in-flight pipeline-step goroutine and wait for them to
	// drain so they don't leak past process exit. Bounded by a deadline —
	// some steps (claude-code, codex) can take minutes, but shutdown must
	// not block forever.
	pipelineShutdownCtx, pipelineShutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := pipelineEngine.Shutdown(pipelineShutdownCtx); err != nil {
		log.Printf("Pipeline engine shutdown incomplete: %v", err)
	}
	pipelineShutdownCancel()
	workerManager.StopAll()
	broker.telegram.StopAll()
	hub.CloseAll()
}

func resolveAgentDBURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envValue := os.Getenv("GOWILD_DATABASE_URL"); envValue != "" {
		return envValue
	}
	return defaultAgentDBURL
}

func loadObjectivesConfigFromEnv() objplan.Config {
	cfg := objplan.NewConfig(
		os.Getenv("GEMINI_API_KEY"),
		os.Getenv("OBJECTIVES_MODEL"),
		os.Getenv("OBJECTIVES_SMART_MODEL"),
	)
	if cfg.Model == "" {
		cfg.Model = os.Getenv("FAST_MODEL")
	}
	if cfg.SmartModel == "" {
		cfg.SmartModel = os.Getenv("SMART_MODEL")
	}
	return cfg
}

func objectivesModelsConfigured(cfg objplan.Config) bool {
	return cfg.Model != "" && cfg.SmartModel != ""
}

func startObjectivesScheduler(ctx context.Context, db gowild_data.Database, geminiClient *loop.GeminiClient) *objplan.Scheduler {
	if geminiClient == nil {
		log.Println("Objectives scheduler disabled (no GEMINI_API_KEY)")
		return nil
	}

	objCfg := loadObjectivesConfigFromEnv()
	if !objectivesModelsConfigured(objCfg) {
		log.Println("Objectives scheduler disabled (FAST_MODEL or SMART_MODEL not set)")
		return nil
	}

	objStore := obj.NewObjectiveStore(db, "")
	objActivity := obj.NewActivityStore(db, "")
	planner, err := objplan.NewStrategicPlanner(objCfg.GeminiAPIKey, objCfg.SmartModel)
	if err != nil {
		log.Printf("Warning: Objectives planner disabled: %v", err)
		return nil
	}

	engine := objplan.NewExecutionEngine(objCfg, objActivity)
	engine.SetCompanyToolLoader(buildCompanyToolLoader(db))
	objMemory := objplan.NewMemoryStore(db)
	evaluator, evalErr := objplan.NewPostExecutionEvaluator(objCfg.GeminiAPIKey, objCfg.SmartModel, objMemory)
	if evalErr != nil {
		log.Printf("Warning: Objectives evaluator disabled: %v", evalErr)
	} else {
		engine.SetEvaluator(evaluator)
	}

	objScheduler := objplan.NewScheduler(objStore, objActivity, planner, engine, objCfg)
	go objScheduler.Run(ctx)
	log.Println("Objectives scheduler started")
	return objScheduler
}

func startHTTPServers(addr, ingressAddr string, handlers *Handlers, broker *BrokerHandlers, webhookRouter *WebhookRouter) {
	server := NewServer(addr, handlers, broker, webhookRouter)
	go runServerOrFatal("Server", server.ListenAndServe)

	if ingressAddr != "" {
		ingressServer := NewIngressServer(ingressAddr, handlers, webhookRouter)
		go runServerOrFatal("Ingress server", ingressServer.ListenAndServe)
	}
}

func runServerOrFatal(name string, run func() error) {
	if err := run(); err != nil {
		log.Fatalf("%s failed: %v", name, err)
	}
}

func waitForBrokerSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("socket %q was not created within %v", path, timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// startWorkersForRunningAgents starts background workers for all agents whose
// containers are running, requeues any previously claimed jobs, and delivers
// queued jobs. On manager restart, agents may have lost in-progress work, so
// we requeue claimed jobs to ensure they are re-delivered.
func startWorkersForRunningAgents(ctx context.Context, service *AgentService, docker *dockermgr.DockerManager, wm *WorkerManager) {
	agents, err := service.ListAgents(ctx)
	if err != nil {
		log.Printf("Failed to list agents for worker startup: %v", err)
		return
	}
	queue := newLocalA2AQueue(service.db)
	for _, agent := range agents {
		if docker.ContainerStatus(ctx, agent.ID) != "running" {
			continue
		}
		go wm.StartAgent(agent.ID)

		// Requeue previously claimed jobs, then deliver all queued jobs.
		agentID := agent.ID
		go func() {
			requeued, err := queue.RequeueAgentClaims(ctx, agentID)
			if err != nil {
				log.Printf("Company method: failed to requeue claimed jobs for %s at startup: %v", agentID, err)
			} else if requeued > 0 {
				log.Printf("Company method: requeued %d previously claimed job(s) for %s at startup", requeued, agentID)
			}
			tools := &BrokerToolsHandler{db: service.db, workerManager: wm}
			delivered, err := tools.deliverQueuedCompanyMethodJobs(ctx, agentID, localA2ADefaultClaimBatch)
			if err != nil {
				log.Printf("Company method: failed queued delivery for %s at startup: %v", agentID, err)
			} else if delivered > 0 {
				log.Printf("Company method: delivered %d queued job(s) for %s at startup", delivered, agentID)
			}
		}()
	}
}

func autoStartAgents(ctx context.Context, service *AgentService, docker *dockermgr.DockerManager, brokerSecret []byte) {
	agents, err := service.ListAutoStartAgents(ctx)
	if err != nil {
		log.Printf("Failed to list auto-start agents: %v", err)
		return
	}

	imageEnsured := false
	for _, agent := range agents {
		volExists, err := docker.VolumeExists(ctx, agent.ID)
		if err != nil {
			log.Printf("Failed to check volume for %s: %v", agent.ID, err)
			continue
		}
		if !volExists {
			log.Printf("Skipping autostart for %s: agent volume missing", agent.ID)
			continue
		}

		if !imageEnsured {
			if err := docker.EnsureImage(ctx); err != nil {
				log.Printf("Failed to build agent image: %v", err)
				return
			}
			imageEnsured = true
		}

		status := docker.ContainerStatus(ctx, agent.ID)
		if status == "running" {
			continue
		}

		log.Printf("Auto-starting agent: %s", agent.ID)

		if status == "" {
			if err := docker.CreateContainer(ctx, buildContainerCreateConfig(agent, brokerSecret)); err != nil {
				log.Printf("Failed to create container for %s: %v", agent.ID, err)
				continue
			}
		}

		if err := docker.StartContainer(ctx, agent.ID); err != nil {
			log.Printf("Failed to start %s: %v", agent.ID, err)
		}
	}
}
