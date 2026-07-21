package main

import (
	"flag"
	"time"
)

const defaultWorkTasksTimeout = 4 * time.Minute

var (
	providerFlag      = flag.String("provider", "", "LLM provider to use (gemini, openai, anthropic)")
	openAIAuthFlag    = flag.String("openai-auth", "", "OpenAI auth mode (api_key or codex_oauth)")
	modelFlag         = flag.String("model", "", "Base model to use (provider-specific)")
	smartModelFlag    = flag.String("smart-model", "", "Smart mode model (provider-specific)")
	systemFlag        = flag.String("system", "", "System prompt")
	maxTurns          = flag.Int("max-turns", 10, "Maximum agentic turns per interaction")
	heartbeatInterval = flag.Duration("heartbeat", 15*time.Minute, "Heartbeat interval (0 to disable)")
	workTasksTimeout  = flag.Duration("worktasks-timeout", defaultWorkTasksTimeout, "Work tasks mode timeout (0 to disable)")
	compactAt         = flag.Int("compact-at", 200000, "Token threshold to trigger context compaction before or during a run")
	compactAfter      = flag.Int("compact-after", 100000, "Target token budget after history compaction")
	keepRecentOutputs = flag.Int("keep-outputs", 3, "Number of recent tool outputs to keep in full during masking")
	agentFlag         = flag.String("agent", "", "Agent ID (default: jake, or GOWILD_AGENT_ID env var)")
	createAgentFlag   = flag.String("create-agent", "", "Create a new agent with this name and exit")
	seedPhraseFlag    = flag.String("seed-phrase", "", "Seed phrase for wallet derivation (used with -create-agent)")
	telegramTokenFlag = flag.String("telegram-token", "", "Set Telegram bot token (use with -agent or -create-agent)")
	emailInboxFlag    = flag.String("email-inbox", "", "Set AgentMail inbox ID (use with -agent or -create-agent)")
	emailAPIKeyFlag   = flag.String("email-apikey", "", "Set AgentMail API key (use with -agent or -create-agent)")
	deleteAgentFlag   = flag.String("delete-agent", "", "Delete an agent and all its data")
	listAgentsFlag    = flag.Bool("list-agents", false, "List all agents and exit")
	responseTimeout   = flag.Duration("response-timeout", 5*time.Minute, "Timeout for each LLM API response (0 to disable)")
	smartFlag         = flag.Bool("smart", false, "Start in smart mode (pro model + extended thinking)")
	logFlag           = flag.Bool("log", false, "Enable session logging to logs/ directory")

	// Sandbox flags
	noSandboxFlag      = flag.Bool("no-sandbox", false, "Run locally without Docker sandbox (debug mode)")
	sandboxRebuildFlag = flag.Bool("sandbox-rebuild", false, "Force rebuild of the sandbox image")
	sandboxBgFlag      = flag.Bool("sandbox-bg", false, "Run sandbox in background (detached)")
	sandboxStopFlag    = flag.Bool("sandbox-stop", false, "Stop an agent's sandbox container")
	sandboxRmFlag      = flag.Bool("sandbox-rm", false, "Remove an agent's sandbox container (keeps data)")
	sandboxPurgeFlag   = flag.Bool("sandbox-purge", false, "Remove sandbox container AND data volume")
	sandboxListFlag    = flag.Bool("sandbox-list", false, "List all sandbox containers")
	sandboxLogsFlag    = flag.Bool("sandbox-logs", false, "Show logs from an agent's sandbox")
	sandboxFollowFlag  = flag.Bool("sandbox-follow", false, "Follow log output (use with -sandbox-logs)")
	sandboxTailFlag    = flag.Int("sandbox-tail", 100, "Number of log lines to show (use with -sandbox-logs)")
	sandboxExecFlag    = flag.String("sandbox-exec", "", "Execute a command in an agent's sandbox")
)
