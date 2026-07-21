package main

import (
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

// AgentResponse is what the API returns for each agent.
type AgentResponse struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	SystemPrompt      string            `json:"system_prompt,omitempty"`
	ModelProvider     string            `json:"model_provider"`
	OpenAIAuthMode    string            `json:"openai_auth_mode"`
	Model             string            `json:"model"`
	SmartModel        string            `json:"smart_model"`
	SmartDefault      bool              `json:"smart_default"`
	Mode              string            `json:"mode"`
	WorkerContextMode string            `json:"worker_context_mode"`
	MaxTurns          int               `json:"max_turns"`
	Heartbeat         string            `json:"heartbeat"`
	WorkTasksTimeout  string            `json:"work_tasks_timeout"`
	ExtraFlags        string            `json:"extra_flags"`
	EnvVars           map[string]string `json:"env_vars"`
	MemoryLimit       string            `json:"memory_limit"`
	CPULimit          string            `json:"cpu_limit"`
	AutoStart         bool              `json:"auto_start"`
	EnabledTools      []string          `json:"enabled_tools"`
	ContainerStatus   string            `json:"container_status"`
	ImageStale        bool              `json:"image_stale"`
	ImageBuildID      string            `json:"image_build_id,omitempty"`
	DesiredBuildID    string            `json:"desired_build_id,omitempty"`
	HasTelegram       bool              `json:"has_telegram"`
	HasEmail          bool              `json:"has_email"`
	AgentNetPublicKey string            `json:"agent_net_public_key,omitempty"`
	AgentNetPremium   bool              `json:"agent_net_premium,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func buildAgentResponse(agent *data.Agent, containerStatus string) AgentResponse {
	mode, workerContextMode := resolveAgentRuntimeConfig(agent.Mode, agent.WorkerContextMode, agent.ExtraFlags)
	return AgentResponse{
		ID:                agent.ID,
		Name:              agent.Name,
		Description:       agent.Description,
		SystemPrompt:      agent.SystemPrompt,
		ModelProvider:     agent.EffectiveModelProvider(),
		OpenAIAuthMode:    agent.EffectiveOpenAIAuthMode(),
		Model:             agent.Model,
		SmartModel:        agent.SmartModel,
		SmartDefault:      agent.SmartDefault,
		Mode:              mode,
		WorkerContextMode: workerContextMode,
		MaxTurns:          agent.MaxTurns,
		Heartbeat:         agent.Heartbeat,
		WorkTasksTimeout:  agent.WorkTasksTimeout,
		ExtraFlags:        agent.ExtraFlags,
		EnvVars:           agent.EnvVars(),
		MemoryLimit:       agent.MemoryLimit,
		CPULimit:          agent.CPULimit,
		AutoStart:         agent.AutoStart,
		EnabledTools:      enabledToolsList(agent),
		ContainerStatus:   containerStatus,
		HasTelegram:       agent.TelegramBotToken != "",
		HasEmail:          agent.AgentMailInboxID != "",
		CreatedAt:         agent.CreatedAt,
		UpdatedAt:         agent.UpdatedAt,
	}
}

// CreateAgentRequest is the request body for creating a new agent.
type CreateAgentRequest struct {
	Name string `json:"name"`
}

// UpdateAgentRequest is the request body for updating manager-specific config.
type UpdateAgentRequest struct {
	ModelProvider     string            `json:"model_provider"`
	OpenAIAuthMode    string            `json:"openai_auth_mode"`
	Model             string            `json:"model"`
	SmartModel        string            `json:"smart_model"`
	SmartDefault      bool              `json:"smart_default"`
	Mode              *string           `json:"mode,omitempty"`
	WorkerContextMode *string           `json:"worker_context_mode,omitempty"`
	MaxTurns          int               `json:"max_turns"`
	Heartbeat         string            `json:"heartbeat"`
	WorkTasksTimeout  string            `json:"work_tasks_timeout"`
	ExtraFlags        *string           `json:"extra_flags,omitempty"`
	EnvVars           map[string]string `json:"env_vars"`
	MemoryLimit       string            `json:"memory_limit"`
	CPULimit          string            `json:"cpu_limit"`
	AutoStart         bool              `json:"auto_start"`
	EnabledTools      *[]string         `json:"enabled_tools,omitempty"`
	SystemPrompt      string            `json:"system_prompt"`
	TelegramBotToken  string            `json:"telegram_bot_token"`
}

// enabledToolsList extracts enabled tool IDs as a slice for JSON serialization.
// Returns nil (JSON null) if not configured, meaning all tools enabled.
func enabledToolsList(agent *data.Agent) []string {
	m := agent.EnabledTools()
	if m == nil {
		return nil
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	return ids
}
