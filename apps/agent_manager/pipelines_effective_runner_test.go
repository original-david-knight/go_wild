package main

import (
	"context"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

// TestEffectiveStepRunnerDoesNotInferFromModelProvider pins the behavior
// decision made when the dead ModelProvider-based fallback was removed from
// effectiveStepRunner: a step with no explicit runner must stay on the A2A
// default ("agent"), even if the target agent's ModelProvider is one that
// an earlier version of the function would have mapped to a CLI runner
// (anthropic → claude-code, openai → codex). Routing to the Codex or
// Claude Code CLI must be explicit — OpenAI is a model provider, Codex is
// one specific CLI tool, and the two must not be silently conflated.
func TestEffectiveStepRunnerDoesNotInferFromModelProvider(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: svc}

	openAIAgent, err := svc.CreateAgent(ctx, "openai-agent")
	if err != nil {
		t.Fatalf("CreateAgent(openai): %v", err)
	}
	openAIAgent.ModelProvider = data.LLMProviderOpenAI
	if err := svc.UpdateAgent(ctx, openAIAgent); err != nil {
		t.Fatalf("UpdateAgent(openai): %v", err)
	}

	anthropicAgent, err := svc.CreateAgent(ctx, "anthropic-agent")
	if err != nil {
		t.Fatalf("CreateAgent(anthropic): %v", err)
	}
	anthropicAgent.ModelProvider = data.LLMProviderAnthropic
	if err := svc.UpdateAgent(ctx, anthropicAgent); err != nil {
		t.Fatalf("UpdateAgent(anthropic): %v", err)
	}

	geminiAgent, err := svc.CreateAgent(ctx, "gemini-agent")
	if err != nil {
		t.Fatalf("CreateAgent(gemini): %v", err)
	}
	geminiAgent.ModelProvider = data.LLMProviderGemini
	if err := svc.UpdateAgent(ctx, geminiAgent); err != nil {
		t.Fatalf("UpdateAgent(gemini): %v", err)
	}

	tests := []struct {
		name    string
		runner  string
		agentID string
		want    string
	}{
		{"empty runner + openai agent stays on agent default", "", "openai-agent", pipelineStepRunnerAgent},
		{"empty runner + anthropic agent stays on agent default", "", "anthropic-agent", pipelineStepRunnerAgent},
		{"empty runner + gemini agent stays on agent default", "", "gemini-agent", pipelineStepRunnerAgent},
		{"explicit agent runner passes through", pipelineStepRunnerAgent, "openai-agent", pipelineStepRunnerAgent},
		{"explicit codex runner passes through", pipelineStepRunnerCodex, "openai-agent", pipelineStepRunnerCodex},
		{"explicit claude-code runner passes through", pipelineStepRunnerClaudeCode, "anthropic-agent", pipelineStepRunnerClaudeCode},
		{"explicit builtin runner passes through", pipelineStepRunnerBuiltin, "", pipelineStepRunnerBuiltin},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := PipelineStep{Runner: tc.runner, ToAgentID: tc.agentID, NextMethod: "m"}
			got := engine.effectiveStepRunner(ctx, step)
			if got != tc.want {
				t.Fatalf("effectiveStepRunner = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEffectiveStepRunnerNormalizesAliases pins that the function goes
// through pipelinespec.NormalizeRunner so every alias the normalizer
// accepts is resolved consistently at the manager layer. This mirrors
// the full alias surface of pipelinespec.NormalizeRunner (spec.go:216).
//
// The "openai" → codex case pins a pre-existing conflation that lives
// one layer down in NormalizeRunner (and is mirrored in the frontend at
// static/app_pipelines.js:690). The removed ModelProvider-based fallback
// in effectiveStepRunner was the same conceptual error (OpenAI is a
// provider, Codex is one specific CLI tool); this lower-layer alias was
// not removed because it is a user-facing runner-field value shared with
// the frontend, and changing it would be a separate, wider user-facing
// behavior change. This test locks the current behavior so any future
// attempt to fix that conflation must update this test (and the
// frontend) deliberately rather than silently.
func TestEffectiveStepRunnerNormalizesAliases(t *testing.T) {
	engine := &PipelineEngine{}

	tests := []struct {
		raw  string
		want string
	}{
		{"", pipelineStepRunnerAgent},
		{"  ", pipelineStepRunnerAgent},
		{"AGENT", pipelineStepRunnerAgent},
		{"claude_code", pipelineStepRunnerClaudeCode},
		{"claudecode", pipelineStepRunnerClaudeCode},
		{"codex-code", pipelineStepRunnerCodex},
		// Pre-existing surviving conflation — documented above.
		{"openai", pipelineStepRunnerCodex},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := engine.effectiveStepRunner(context.Background(), PipelineStep{Runner: tc.raw})
			if got != tc.want {
				t.Fatalf("effectiveStepRunner(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestEffectiveStepRunnerWhitespaceRunnerWithAgentLookup pins that a
// whitespace-only Runner does NOT trigger ModelProvider lookup on the
// target agent. Codex review flagged this gap in the behavior-pin
// test: the main "does not infer" test uses literal "" only, so it
// didn't prove the whitespace branch of NormalizeRunner also bypasses
// provider inference.
func TestEffectiveStepRunnerWhitespaceRunnerWithAgentLookup(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: svc}

	agent, err := svc.CreateAgent(ctx, "ws-openai-agent")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	agent.ModelProvider = data.LLMProviderOpenAI
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	for _, raw := range []string{" ", "\t", "  \n  "} {
		t.Run(raw, func(t *testing.T) {
			step := PipelineStep{Runner: raw, ToAgentID: "ws-openai-agent", NextMethod: "m"}
			if got := engine.effectiveStepRunner(ctx, step); got != pipelineStepRunnerAgent {
				t.Fatalf("effectiveStepRunner(runner=%q, openai agent) = %q, want %q", raw, got, pipelineStepRunnerAgent)
			}
		})
	}
}
