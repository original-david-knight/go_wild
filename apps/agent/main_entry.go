package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fatih/color"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/my"
	"github.com/original-david-knight/go_wild/tools"
	"github.com/original-david-knight/go_wild/tools/broker"
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal - will be set up after readline is created
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Load .env file
	envPath := gowild_my.LoadEnv()
	if envPath != "" {
		fmt.Println(color.HiBlackString("Loaded: %s", envPath))
	}

	// Handle -create-agent flag
	if *createAgentFlag != "" {
		return createNewAgent(ctx, *createAgentFlag, *seedPhraseFlag, *telegramTokenFlag, *emailInboxFlag)
	}

	// Handle -telegram-token flag (set token for existing agent)
	if *telegramTokenFlag != "" && *createAgentFlag == "" {
		return setTelegramToken(ctx, getAgentID(), *telegramTokenFlag)
	}

	// Handle -email-apikey flag (set API key for existing agent)
	if *emailAPIKeyFlag != "" && *createAgentFlag == "" {
		return setEmailAPIKey(ctx, getAgentID(), *emailAPIKeyFlag)
	}

	// Handle -email-inbox flag (set inbox for existing agent)
	if *emailInboxFlag != "" && *createAgentFlag == "" {
		return setEmailInbox(ctx, getAgentID(), *emailInboxFlag)
	}

	// Handle -list-agents flag
	if *listAgentsFlag {
		return listAgents(ctx)
	}

	// Handle -delete-agent flag
	if *deleteAgentFlag != "" {
		return deleteAgent(ctx, *deleteAgentFlag)
	}

	// Handle sandbox management commands
	if *sandboxListFlag {
		return handleSandboxList(ctx)
	}

	// Get agent ID early for sandbox commands that need it
	agentID := getAgentID()

	if *sandboxStopFlag {
		return StopSandbox(ctx, agentID)
	}
	if *sandboxRmFlag {
		return RemoveSandbox(ctx, agentID)
	}
	if *sandboxPurgeFlag {
		return PurgeSandbox(ctx, agentID)
	}
	if *sandboxLogsFlag {
		return LogsSandbox(ctx, agentID, *sandboxFollowFlag, *sandboxTailFlag)
	}
	if *sandboxExecFlag != "" {
		return ExecInSandbox(ctx, agentID, strings.Fields(*sandboxExecFlag), true)
	}

	// Default to sandbox mode unless -no-sandbox is set
	if !*noSandboxFlag && !tools.IsInContainer() {
		exePath, _ := os.Executable()
		if strings.Contains(exePath, "go-build") {
			fmt.Println(color.YellowString("Warning: sandbox requires a built binary. Running locally."))
		} else if err := exec.Command("docker", "version").Run(); err != nil {
			fmt.Println(color.YellowString("Warning: Docker not available. Running locally without sandbox."))
		} else {
			return runInSandbox(ctx, agentID)
		}
	}

	output.System("Agent: %s", agentID)

	// Initialize session logger if -log flag is set
	if *logFlag {
		cleanOldLogs()
		logger, err := NewSessionLogger(agentID)
		if err != nil {
			output.SystemWarning("Warning: session logging failed: %v", err)
		} else {
			globalSessionLogger = logger
			defer logger.Close()
			output.System("Logging: %s", logger.path)
		}
	}

	runtime := initializeAgentRuntime(ctx, agentID)
	defer runtime.close()
	if err := runtime.startupError(); err != nil {
		return err
	}

	globalBrokerClient = runtime.brokerClient
	globalAgentService = runtime.service

	switch {
	case runtime.usingBroker():
		output.System("Broker: %s", runtime.brokerClient.Endpoint())
		output.System("Database: via broker")
	case runtime.usingDirectService():
		output.SystemWarning("Broker unavailable; running direct.")
		output.System("Database: direct")
	default:
		output.SystemWarning("Database unavailable; continuing without database-backed tools.")
	}

	// Build options
	var opts []loop.Option
	systemPrompt := loadSystemPrompt(ctx, agentID, runtime)
	if *systemFlag != "" {
		// Command-line flag takes precedence
		opts = append(opts, loop.WithSystemPrompt(*systemFlag))
	} else {
		opts = append(opts, loop.WithSystemPrompt(systemPrompt))
	}
	opts = append(opts, loop.WithMaxTurns(*maxTurns))
	// Set context limit higher than compaction thresholds - compaction will trigger first
	maxCompact := *compactAt
	if *compactAfter > maxCompact {
		maxCompact = *compactAfter
	}
	opts = append(opts, loop.WithMaxContextTokens(maxCompact*2))
	// Mid-run compaction: use maskObservations when tokens exceed threshold during a run
	keepOutputs := *keepRecentOutputs
	opts = append(opts, loop.WithCompaction(*compactAt, func(history []loop.Message, promptTokens int) ([]loop.Message, error) {
		result := maskObservations(history, keepOutputs)
		return result.MaskedHistory, nil
	}))
	// Set response timeout to prevent freezing on slow API responses
	if *responseTimeout > 0 {
		opts = append(opts, loop.WithResponseTimeout(*responseTimeout))
	}

	// Resolve provider/model names from explicit config and persisted agent settings.
	llmConfig, err := resolveModelConfig(ctx, runtime)
	if err != nil {
		return err
	}
	globalLLMConfig = llmConfig

	// Use the broker LLM client — start with smart model if -smart flag is set
	initialModel, err := llmConfig.initialModel(*smartFlag)
	if err != nil {
		return err
	}
	if runtime.usingBroker() {
		llmClient := broker.NewLLMClient(runtime.brokerClient, initialModel)
		opts = append(opts, loop.WithLLMClient(llmClient))
		output.System("LLM: via broker (%s)", llmConfig.Provider)
	} else {
		llmClient, err := loop.NewProviderClient(ctx, loop.ProviderClientConfig{
			Provider:       llmConfig.Provider,
			Model:          initialModel,
			OpenAIAuthMode: llmConfig.OpenAIAuthMode,
		})
		if err != nil {
			return fmt.Errorf("failed to create %s client: %w", llmConfig.Provider, err)
		}
		opts = append(opts, loop.WithLLMClient(llmClient))
		output.System("LLM: direct %s", llmConfig.Provider)
	}

	// Create the agent. In broker mode the broker handles provider-specific model
	// access; in direct mode we inject a provider-aware client above.
	agent, err := loop.New(ctx, "", initialModel, opts...)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer agent.Close()

	addTools(ctx, agent, runtime)
	defer closeMCPClients()

	// Cleanup telegram on exit
	defer func() {
		if globalTelegramTools != nil {
			globalTelegramTools.Stop()
		}
	}()

	smartMode := *smartFlag
	modelConfig := modelPair{base: llmConfig.BaseModel, smart: llmConfig.SmartModel}

	return runInteractiveSession(ctx, sigCh, agent, agentID, modelConfig, &smartMode)
}
