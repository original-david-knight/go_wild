package data

import (
	"encoding/json"
	"strings"
	"time"
)

// A2AMethod defines a globally-available A2A method (name + optional payload schemas).
//
// Method is the primary key (stable identifier) and is referenced by AgentCapability.Method.
type A2AMethod struct {
	Method                            string    `json:"method" db:"id"`
	Description                       string    `json:"description"`
	Instructions                      string    `json:"instructions"`
	InputSchemaJSON                   string    `json:"input_schema_json"`
	OutputSchemaJSON                  string    `json:"output_schema_json"`
	ModelTier                         string    `json:"model_tier" db:"model_tier"` // "fast" or "smart" (default: "smart")
	AutoMarketNote                    bool      `json:"auto_market_note" db:"auto_market_note"`
	FreshContext                      bool      `json:"fresh_context" db:"fresh_context"`
	RedactMarketPrices                bool      `json:"redact_market_prices" db:"redact_market_prices"`
	DisableMarketNotes                bool      `json:"disable_market_notes" db:"disable_market_notes"`
	DisablePolymarketNoteAugmentation bool      `json:"disable_polymarket_note_augmentation" db:"disable_polymarket_note_augmentation"`
	DisabledToolGroupsJSON            string    `json:"disabled_tool_groups_json" db:"disabled_tool_groups_json"`
	CompletionTimestampKey string `json:"completion_timestamp_key" db:"completion_timestamp_key"`
	CompletionSuccessKey  string `json:"completion_success_key" db:"completion_success_key"`
	CreatedAt                         time.Time `json:"created_at"`
	UpdatedAt                         time.Time `json:"updated_at"`
}

func (A2AMethod) TableName() string { return "a2a_methods" }

func (m A2AMethod) MarketNotesDisabled() bool {
	return m.DisableMarketNotes || m.IsToolGroupDisabled("polymarket_notes")
}

func (m A2AMethod) PolymarketNoteAugmentationDisabled() bool {
	return m.DisableMarketNotes || m.DisablePolymarketNoteAugmentation || m.IsToolGroupDisabled("polymarket_notes")
}

// DisabledToolGroups returns the list of tool group IDs disabled for this method.
func (m A2AMethod) DisabledToolGroups() []string {
	raw := strings.TrimSpace(m.DisabledToolGroupsJSON)
	if raw == "" {
		return nil
	}
	var groups []string
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		return nil
	}
	return groups
}

// IsToolGroupDisabled returns true if the given tool group is disabled for this method.
func (m A2AMethod) IsToolGroupDisabled(groupID string) bool {
	for _, g := range m.DisabledToolGroups() {
		if strings.TrimSpace(g) == groupID {
			return true
		}
	}
	return false
}

// DisabledToolNames returns the set of tool names that are disabled for this method
// based on its disabled tool groups.
func (m A2AMethod) DisabledToolNames() map[string]struct{} {
	groups := m.DisabledToolGroups()
	if len(groups) == 0 {
		return nil
	}
	disabled := make(map[string]struct{})
	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[strings.TrimSpace(g)] = struct{}{}
	}
	for _, tg := range AllToolGroups {
		if _, ok := groupSet[tg.ID]; ok {
			for _, tool := range tg.Tools {
				disabled[tool] = struct{}{}
			}
		}
	}
	return disabled
}

// AgentCapability represents a capability registered by an agent.
// Used by the manager to route A2A requests to the right agent.
type AgentCapability struct {
	ID               string    `json:"id"`
	AgentID          string    `json:"agent_id"`
	Role             string    `json:"role"`
	Method           string    `json:"method"`
	Description      string    `json:"description"`
	InputSchemaJSON  string    `json:"input_schema_json"`
	OutputSchemaJSON string    `json:"output_schema_json"`
	RegisteredAt     time.Time `json:"registered_at"`
}

// SpendEntry represents a single spend event tracked by the spend ledger.
type SpendEntry struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Category  string    `json:"category"` // "ads", "orders", "shopify", "llm"
	Amount    float64   `json:"amount"`
	ToolName  string    `json:"tool_name"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// SpendLimit defines a daily spend cap per agent per category.
type SpendLimit struct {
	ID         string  `json:"id"`
	AgentID    string  `json:"agent_id"`
	Category   string  `json:"category"`
	DailyLimit float64 `json:"daily_limit"`
}

// PipelineDefinition defines a multi-step pipeline of A2A jobs.
type PipelineDefinition struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	StepsJSON      string    `json:"steps_json"`       // Serialized []PipelineStep
	ScopeMode      string    `json:"scope_mode"`       // "global" (default) or "company"
	ScopeCompanyID string    `json:"scope_company_id"` // Required when scope_mode is "company"
	Schedule       string    `json:"schedule"`         // Go duration string e.g. "4h", "30m", "" for manual-only
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PipelineRun tracks a single execution of a pipeline.
type PipelineRun struct {
	ID             string    `json:"id"`
	PipelineID     string    `json:"pipeline_id"`
	TriggerJobID   string    `json:"trigger_job_id"`
	ScopeMode      string    `json:"scope_mode"`       // Snapshot from definition: "global" or "company"
	ScopeCompanyID string    `json:"scope_company_id"` // Snapshot from definition when scope_mode is "company"
	CurrentStep    int       `json:"current_step"`
	Status         string    `json:"status"` // "running", "completed", "failed"
	FailureReason  string    `json:"failure_reason"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PipelineStepRun tracks an individual step within a pipeline run.
type PipelineStepRun struct {
	ID          string    `json:"id"`
	RunID       string    `json:"run_id"`
	StepIndex   int       `json:"step_index"`
	A2AJobID    string    `json:"a2a_job_id"`
	Status      string    `json:"status"` // "pending", "running", "succeeded", "failed"
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// WebhookConfig maps an incoming webhook event to a target agent capability.
type WebhookConfig struct {
	ID           string `json:"id"`
	CompanyID    string `json:"company_id,omitempty"`
	Source       string `json:"source"` // "shopify", "stripe", etc.
	Event        string `json:"event"`  // "orders/create", etc.
	EventPath    string `json:"event_path,omitempty"`
	TargetRole   string `json:"target_role"`
	TargetMethod string `json:"target_method"`
	Enabled      bool   `json:"enabled"`
	HMACSecret   string `json:"hmac_secret"`
}

// WebhookEvent tracks an incoming webhook for deduplication and retry.
type WebhookEvent struct {
	ID          string    `json:"id"`
	EventID     string    `json:"event_id"` // External event ID (e.g. X-Shopify-Event-Id)
	CompanyID   string    `json:"company_id,omitempty"`
	Source      string    `json:"source"`
	Topic       string    `json:"topic"`
	PayloadJSON string    `json:"payload_json"`
	Status      string    `json:"status"` // "pending", "delivered", "failed", "dead_letter"
	Attempts    int       `json:"attempts"`
	NextRetryAt time.Time `json:"next_retry_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
