package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	data "github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

type brokerLLMClientFactory func(ctx context.Context, agent *data.Agent, model string) (loop.LLMClient, error)

// BrokerLLMHandler handles LLM proxy requests.
type BrokerLLMHandler struct {
	service     *AgentService
	newClient   brokerLLMClientFactory
	rateLimiter *llmRateLimiter
}

// NewBrokerLLMHandler creates a new LLM broker handler.
func NewBrokerLLMHandler(service *AgentService) *BrokerLLMHandler {
	return &BrokerLLMHandler{
		service: service,
		newClient: func(ctx context.Context, agent *data.Agent, model string) (loop.LLMClient, error) {
			return loop.NewProviderClient(ctx, loop.ProviderClientConfig{
				Provider:       agent.EffectiveModelProvider(),
				Model:          model,
				OpenAIAuthMode: agent.EffectiveOpenAIAuthMode(),
			})
		},
		rateLimiter: newLLMRateLimiter(5),
	}
}

// BrokerLLMRequest is the JSON request body for /broker/v1/llm/generate.
type BrokerLLMRequest struct {
	// Contents and config are serialized using the agentic_loop serialization format.
	Contents          []loop.SerializedContent `json:"contents"`
	SystemInstruction string                   `json:"system_instruction,omitempty"`
	Tools             json.RawMessage          `json:"tools,omitempty"` // genai tools JSON
	Temperature       *float32                 `json:"temperature,omitempty"`
	MaxOutputTokens   int32                    `json:"max_output_tokens,omitempty"`
	ThinkingBudget    int32                    `json:"thinking_budget,omitempty"`
	Model             string                   `json:"model,omitempty"`
}

// BrokerLLMResponse is the JSON response from /broker/v1/llm/generate.
type BrokerLLMResponse struct {
	Content      *loop.SerializedContent `json:"content,omitempty"`
	Usage        *loop.ModelUsage        `json:"usage,omitempty"`
	FinishReason string                  `json:"finish_reason,omitempty"`
}

// handleGenerate handles POST /broker/v1/llm/generate.
func (h *BrokerLLMHandler) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}
	if h.service == nil || h.newClient == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM client unavailable")
		return
	}

	var req BrokerLLMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Deserialize contents from serialized format to genai.Content
	contents, err := loop.DeserializeContents(req.Contents)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to deserialize contents: "+err.Error())
		return
	}

	// Build config
	config := &loop.GenerateContentConfig{
		SystemInstruction: req.SystemInstruction,
		Temperature:       req.Temperature,
		MaxOutputTokens:   req.MaxOutputTokens,
		ThinkingBudget:    req.ThinkingBudget,
	}

	// Deserialize tools if provided
	if len(req.Tools) > 0 {
		tools, err := loop.DeserializeTools(req.Tools)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to deserialize tools: "+err.Error())
			return
		}
		config.Tools = tools
	}

	agent, err := h.service.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	config.Model = resolveBrokerRequestedModel(agent, req.Model)

	client, err := h.newClient(r.Context(), agent, config.Model)
	if err != nil {
		log.Printf("Broker LLM client init error for agent %s (provider=%s model=%s): %v", agentID, agent.EffectiveModelProvider(), config.Model, err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM client init failed: %v", err))
		return
	}
	defer client.Close()

	// Count tools for logging
	toolCount := 0
	if config.Tools != nil {
		for _, t := range config.Tools {
			toolCount += len(t.FunctionDeclarations)
		}
	}

	// Acquire rate limiter slot (waits for cooldown + concurrency)
	release, err := h.rateLimiter.Acquire(r.Context())
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, fmt.Sprintf("rate limiter cancelled: %v", err))
		return
	}
	defer release()

	model := config.Model
	provider := agent.EffectiveModelProvider()

	resp, err := client.GenerateContent(r.Context(), contents, config)
	if err != nil {
		if isLLMRateLimitError(err) {
			h.rateLimiter.RecordRateLimit()
		}
		log.Printf("Broker LLM error for agent %s (provider=%s model=%s messages=%d tools=%d): %v", agentID, provider, model, len(contents), toolCount, err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("LLM generation failed: %v", err))
		return
	}
	h.rateLimiter.RecordSuccess()

	// Log token usage
	if resp.Usage != nil {
		log.Printf("LLM [%s] provider=%s model=%s prompt=%d completion=%d total=%d messages=%d tools=%d",
			agentID, provider, model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, len(contents), toolCount)
	}

	// Serialize response
	brokerResp := BrokerLLMResponse{
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
	}
	if resp.Content != nil {
		serialized := loop.SerializeContent(resp.Content)
		brokerResp.Content = &serialized
	}

	writeJSON(w, http.StatusOK, brokerResp)
}

func resolveBrokerRequestedModel(agent *data.Agent, requested string) string {
	provider := data.LLMProviderGemini
	if agent != nil {
		provider = agent.EffectiveModelProvider()
	}

	requested = loop.NormalizeModelForProvider(provider, requested)
	if requested != "" {
		return requested
	}
	if agent != nil {
		if model := loop.NormalizeModelForProvider(provider, agent.Model); model != "" {
			return model
		}
		return loop.DefaultModelForProvider(provider)
	}
	return loop.DefaultModelForProvider(provider)
}
