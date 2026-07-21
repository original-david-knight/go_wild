// Package data provides data models and database operations for the agent.
package data

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	LLMProviderGemini    = "gemini"
	LLMProviderOpenAI    = "openai"
	LLMProviderAnthropic = "anthropic"

	OpenAIAuthModeAPIKey     = "api_key"
	OpenAIAuthModeCodexOAuth = "codex_oauth"
)

// Agent represents an agent configuration.
type Agent struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	SystemPrompt string         `json:"system_prompt,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	// Wallet seed phrase - keys are derived at runtime using BIP39/SLIP-0010
	// Derivation paths: ETH m/44'/60'/0'/0/0, SOL m/44'/501'/0'/0'
	// TODO: Should be encrypted at rest in production
	WalletSeedPhrase string `json:"wallet_seed_phrase,omitempty"`

	// Telegram bot token - obtained from @BotFather
	// Each agent can have its own Telegram bot for communication
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`

	// AgentMail API key - obtained from AgentMail console
	AgentMailAPIKey string `json:"agentmail_api_key,omitempty"`

	// AgentMail inbox ID - obtained from AgentMail console or API
	// Each agent has its own email inbox for communication
	AgentMailInboxID string `json:"agentmail_inbox_id,omitempty"`

	// Report HTML rendered in the manager UI report tab
	ReportHTML      string    `json:"report_html,omitempty"`
	ReportUpdatedAt time.Time `json:"report_updated_at,omitempty"`

	// Manager configuration - used by apps/agent_manager for container deployment
	ModelProvider    string `json:"model_provider,omitempty"`
	OpenAIAuthMode   string `json:"openai_auth_mode,omitempty"`
	Model            string `json:"model,omitempty"`
	SmartModel       string `json:"smart_model,omitempty"`
	SmartDefault     bool   `json:"smart_default"`
	MaxTurns         int    `json:"max_turns"`
	Heartbeat        string `json:"heartbeat,omitempty"`
	WorkTasksTimeout string `json:"work_tasks_timeout,omitempty"`
	ExtraFlags       string `json:"extra_flags,omitempty"`
	EnvVarsJSON      string `json:"env_vars_json,omitempty"`
	EnabledToolsJSON string `json:"enabled_tools_json,omitempty"`
	MemoryLimit      string `json:"memory_limit,omitempty"`
	CPULimit         string `json:"cpu_limit,omitempty"`
	AutoStart        bool   `json:"auto_start"`

	// Worker mode configuration
	Mode              string `json:"mode,omitempty"`                // "interactive" or "worker"
	WorkerContextMode string `json:"worker_context_mode,omitempty"` // "stateless" or "persistent"
}

// EffectiveModelProvider returns the configured provider or the Gemini default.
func (a *Agent) EffectiveModelProvider() string {
	return NormalizeLLMProvider(a.ModelProvider)
}

// EffectiveOpenAIAuthMode returns the configured OpenAI auth mode or the API key default.
func (a *Agent) EffectiveOpenAIAuthMode() string {
	return NormalizeOpenAIAuthMode(a.OpenAIAuthMode)
}

// NormalizeLLMProvider canonicalizes a persisted provider value.
func NormalizeLLMProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", LLMProviderGemini:
		return LLMProviderGemini
	case LLMProviderOpenAI:
		return LLMProviderOpenAI
	case LLMProviderAnthropic:
		return LLMProviderAnthropic
	default:
		return LLMProviderGemini
	}
}

// NormalizeOpenAIAuthMode canonicalizes a persisted OpenAI auth mode.
func NormalizeOpenAIAuthMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", OpenAIAuthModeAPIKey:
		return OpenAIAuthModeAPIKey
	case OpenAIAuthModeCodexOAuth:
		return OpenAIAuthModeCodexOAuth
	default:
		return OpenAIAuthModeAPIKey
	}
}

// EnvVars returns the parsed environment variables map.
func (a *Agent) EnvVars() map[string]string {
	if a.EnvVarsJSON == "" {
		return nil
	}
	var env map[string]string
	json.Unmarshal([]byte(a.EnvVarsJSON), &env)
	return env
}

// SetEnvVars encodes a map into the JSON field.
func (a *Agent) SetEnvVars(env map[string]string) {
	if len(env) == 0 {
		a.EnvVarsJSON = ""
		return
	}
	d, _ := json.Marshal(env)
	a.EnvVarsJSON = string(d)
}

// EnabledTools returns the set of enabled tool group IDs.
// Returns nil if no tools have been explicitly configured (meaning all enabled for new agents).
func (a *Agent) EnabledTools() map[string]bool {
	if a.EnabledToolsJSON == "" {
		return nil
	}
	var ids []string
	json.Unmarshal([]byte(a.EnabledToolsJSON), &ids)
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// SetEnabledTools encodes a list of enabled tool group IDs into the JSON field.
func (a *Agent) SetEnabledTools(ids []string) {
	if ids == nil {
		a.EnabledToolsJSON = ""
		return
	}
	d, _ := json.Marshal(ids)
	a.EnabledToolsJSON = string(d)
}

// CommandMessage is a parsed slash command, relayed as structured JSON
// from the manager UI to the agent via stdin.
type CommandMessage struct {
	Type    string         `json:"type"`
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
	Raw     string         `json:"raw,omitempty"`
}

// StatusRequestMessage asks the agent to emit a specific runtime status.
// Example: { "type": "status_request", "name": "smart_mode" }
type StatusRequestMessage struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// RuntimeStatus represents the agent's current runtime state.
// Emitted as a JSON line with type:"runtime_status" and cached by the manager
// for REST replay to newly-connected clients.
type RuntimeStatus struct {
	Type      string `json:"type"`  // always "runtime_status"
	State     string `json:"state"` // "idle", "thinking", "responding", "tool_running"
	SmartMode bool   `json:"smart_mode"`
	Model     string `json:"model"`
}

// MemoryEntry represents short-term memory for an agent.
type MemoryEntry struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ArchiveEntry represents long-term memory archive for an agent.
type ArchiveEntry struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Summary   string    `json:"summary"`
	Tags      string    `json:"tags"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Soul represents the agent's identity and values.
type Soul struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// Skill represents a saved Python skill for an agent.
type Skill struct {
	ID           string            `json:"id"`
	AgentID      string            `json:"agent_id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  map[string]string `json:"input_schema"`
	Code         string            `json:"code"`
	Dependencies []string          `json:"dependencies,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// WalletTransaction represents a logged blockchain transaction.
type WalletTransaction struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agent_id"`
	Timestamp       time.Time      `json:"timestamp"`
	Chain           string         `json:"chain"`
	Type            string         `json:"type"` // sign_message, send_token, swap_token, contract_call
	FromAddress     string         `json:"from_address"`
	ToAddress       string         `json:"to_address,omitempty"`
	Amount          string         `json:"amount,omitempty"`
	TokenAddress    string         `json:"token_address,omitempty"`
	TransactionHash string         `json:"transaction_hash,omitempty"`
	Status          string         `json:"status"` // pending, confirmed, failed
	Error           string         `json:"error,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExplorerURL     string         `json:"explorer_url,omitempty"`
}

// Task represents an agent task for tracking work.
type Task struct {
	ID              string     `json:"id"`
	AgentID         string     `json:"agent_id"`
	Description     string     `json:"description"`
	Status          string     `json:"status"` // pending, done, deprecated
	Blocked         bool       `json:"blocked"`
	SleepUntil      *time.Time `json:"sleep_until,omitempty"`       // If set, task is sleeping until this time
	Position        int        `json:"position"`                    // For ordering tasks
	RecurringTaskID string     `json:"recurring_task_id,omitempty"` // If set, this task came from a recurring task
	ParentTaskID    string     `json:"parent_task_id,omitempty"`    // Links subtask to parent goal
	Outcome         string     `json:"outcome,omitempty"`           // Result/findings persisted across sessions
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// RecurringTask represents a template for tasks that recur on a schedule.
// The agent framework periodically checks these and creates new tasks from them.
type RecurringTask struct {
	ID              string    `json:"id"`
	AgentID         string    `json:"agent_id"`
	Description     string    `json:"description"`
	IntervalMinutes int       `json:"interval_minutes"` // Minutes between recurrences
	LastCreatedAt   time.Time `json:"last_created_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// HistorySnapshot stores the serialized agent history for rehydration after restart.
// Payload is a JSON-encoded slice of serialized loop messages.
type HistorySnapshot struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Payload   string    `json:"payload"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HistorySummary stores the extracted summary text separately from the snapshot.
// When history is compacted, the summary message is persisted here so that on
// rehydration the agent loads only summary + non-summarized messages.
type HistorySummary struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// ChatMessage represents a saved chat message (user prompt or assistant response).
type ChatMessage struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// TelegramMessageRecord represents a stored Telegram message for persistence.
// Messages are stored by the manager's TelegramWorker and deduplicated by UpdateID.
type TelegramMessageRecord struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	UpdateID  int64     `json:"update_id"`
	MessageID int64     `json:"message_id"`
	ChatID    int64     `json:"chat_id"`
	ChatType  string    `json:"chat_type"`
	ChatTitle string    `json:"chat_title"`
	FromID    int64     `json:"from_id"`
	FromName  string    `json:"from_name"`
	Username  string    `json:"username"`
	Text      string    `json:"text"`
	Date      time.Time `json:"date"`
	ReplyToID int64     `json:"reply_to_id"`
	CreatedAt time.Time `json:"created_at"`
}

// PendingEmail represents an outgoing email queued for user approval.
// Emails are intercepted before sending and stored here until the user
// approves or rejects them via /outbox commands.
type PendingEmail struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	Type        string    `json:"type"`       // "send", "reply", "forward"
	Recipients  string    `json:"recipients"` // comma-separated for display
	Subject     string    `json:"subject"`
	Preview     string    `json:"preview"`      // first ~100 chars of body
	RequestData string    `json:"request_data"` // JSON of original input struct
	Status      string    `json:"status"`       // "pending", "approved", "rejected"
	CreatedAt   time.Time `json:"created_at"`
}

// EmailWhitelistEntry represents a whitelisted email address for an agent.
// Recipients on the whitelist bypass the email approval queue.
type EmailWhitelistEntry struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// PeerGroup is a named group; agents sharing a group can message each other.
type PeerGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// PeerGroupMember links an agent to a peer group.
type PeerGroupMember struct {
	ID        string    `json:"id"`
	GroupID   string    `json:"group_id"`
	AgentID   string    `json:"agent_id"`
	CreatedAt time.Time `json:"created_at"`
}

// AgentMessage is a message between two agents (never deleted).
type AgentMessage struct {
	ID          string     `json:"id"`
	FromAgentID string     `json:"from_agent_id"`
	ToAgentID   string     `json:"to_agent_id"`
	Content     string     `json:"content"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}
