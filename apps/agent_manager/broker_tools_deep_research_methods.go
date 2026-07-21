package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

func (h *BrokerToolsHandler) callDeepResearchMethodTools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	_ = svc

	spec, ok, err := deepResearchToolSpecForName(ctx, h.db, toolName)
	if err != nil {
		return true, nil, err
	}
	if !ok {
		return false, nil, nil
	}

	methodDef, err := NewAgentService(h.db).GetDeepResearchMethod(ctx, spec.Method)
	if err != nil {
		return true, nil, err
	}
	if methodDef == nil || !methodDef.Enabled {
		return true, nil, fmt.Errorf("deep research method %q is disabled", spec.Method)
	}

	input := map[string]any{}
	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &input); err != nil {
			return true, nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}
		if input == nil {
			input = map[string]any{}
		}
	}

	started := time.Now()
	timeout := deepResearchTimeoutForMethod(methodDef)
	runCtx, cancel := detachedDeepResearchContext(ctx, h.shutdownCtx, timeout)
	defer cancel()

	log.Printf("[deep-research] agent=%s method=%s starting (timeout=%s)", agentID, spec.Method, timeout)

	progress := func(event deepresearch.ProgressEvent) {
		switch event.Stage {
		case "planned_query":
			log.Printf("[deep-research] agent=%s method=%s round=%d search %s: %s", agentID, spec.Method, event.Round, event.ObjectiveKey, event.Query)
		case "source":
			log.Printf("[deep-research] agent=%s method=%s round=%d source %s: %s (%s)", agentID, spec.Method, event.Round, event.ObjectiveKey, event.Title, event.URL)
		case "round_complete":
			log.Printf("[deep-research] agent=%s method=%s round=%d complete", agentID, spec.Method, event.Round)
		case "warning":
			log.Printf("[deep-research] agent=%s method=%s warning: %s", agentID, spec.Method, event.Warning)
		}
	}

	llmBackend, err := resolveAgentLLMBackendForDeepResearch(ctx, h.db, agentID)
	if err != nil {
		return true, nil, err
	}

	optionsOverride, _ := json.Marshal(map[string]any{"llm_backend": llmBackend})
	out, err := deepResearchRunMethodTestProgress(runCtx, methodDef, deepResearchMethodTestRequest{
		Input:   input,
		Options: optionsOverride,
	}, progress)
	elapsed := time.Since(started)
	if err != nil {
		log.Printf("[deep-research] agent=%s method=%s failed after %s: %v", agentID, spec.Method, elapsed.Round(time.Millisecond), err)
		return true, nil, err
	}
	_ = NewAgentService(h.db).MarkDeepResearchMethodTested(ctx, spec.Method)

	log.Printf("[deep-research] agent=%s method=%s completed in %s", agentID, spec.Method, elapsed.Round(time.Millisecond))

	// Return only the structured output defined by the method's research schema.
	// The full Result contains findings with large excerpts that blow out
	// the agent's context window.
	return true, map[string]any{
		"ok":          true,
		"method":      strings.TrimSpace(spec.Method),
		"result":      out.Result.Output,
		"duration_ms": elapsed.Milliseconds(),
	}, nil
}

// resolveAgentLLMBackendForDeepResearch maps the agent's configured model
// provider to the deep research LLM backend name.
func resolveAgentLLMBackendForDeepResearch(ctx context.Context, db gowild_data.Database, agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", fmt.Errorf("deep research searcher unavailable: agent ID is required")
	}
	agent, err := data.NewAgentService(db, agentID).GetAgent(ctx)
	if err != nil {
		return "", fmt.Errorf("deep research searcher unavailable: failed to load agent %q: %w", agentID, err)
	}
	provider := strings.ToLower(strings.TrimSpace(agent.ModelProvider))
	switch provider {
	case data.LLMProviderAnthropic:
		return "claude", nil
	case data.LLMProviderGemini:
		return "gemini", nil
	case data.LLMProviderOpenAI:
		return "codex", nil
	case "":
		return "", fmt.Errorf("deep research searcher unavailable: agent %q has no model_provider configured (set it to %q, %q, or %q)", agentID, data.LLMProviderAnthropic, data.LLMProviderGemini, data.LLMProviderOpenAI)
	default:
		return "", fmt.Errorf("deep research searcher unavailable: agent %q uses unsupported LLM provider %q", agentID, provider)
	}
}
