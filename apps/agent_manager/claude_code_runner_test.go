package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/claudellm"
	codexllm "github.com/original-david-knight/go_wild/codexllm"
)

func TestPrepareClaudeOAuthSandboxHomeCopiesCredentials(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	claudeDir := filepath.Join(userHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(`{"token":"personal-oauth"}`), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userHome, ".claude.json"), []byte(`{"setting":true}`), 0600); err != nil {
		t.Fatalf("write claude config: %v", err)
	}

	sandboxHome, cleanup, err := prepareClaudeOAuthSandboxHome()
	if err != nil {
		t.Fatalf("prepareClaudeOAuthSandboxHome returned error: %v", err)
	}
	defer cleanup()

	gotCredentials, err := os.ReadFile(filepath.Join(sandboxHome, ".claude", ".credentials.json"))
	if err != nil {
		t.Fatalf("read copied credentials: %v", err)
	}
	if string(gotCredentials) != `{"token":"personal-oauth"}` {
		t.Fatalf("copied credentials = %q", string(gotCredentials))
	}

	gotConfig, err := os.ReadFile(filepath.Join(sandboxHome, ".claude.json"))
	if err != nil {
		t.Fatalf("read copied claude config: %v", err)
	}
	if string(gotConfig) != `{"setting":true}` {
		t.Fatalf("copied config = %q", string(gotConfig))
	}
}

func TestPrepareClaudeOAuthSandboxHomeRequiresCredentials(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	_, cleanup, err := prepareClaudeOAuthSandboxHome()
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatalf("expected missing credentials error")
	}
	if !strings.Contains(err.Error(), "personal Claude OAuth credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildClaudePipelineSandboxCommandUsesScratchDirAndDisallowsFileTools(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	claudeDir := filepath.Join(userHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(`{"token":"personal-oauth"}`), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	binDir := t.TempDir()
	bwrapPath := filepath.Join(binDir, "bwrap")
	if err := os.WriteFile(bwrapPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	claudePath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	mcpBrokerPath := filepath.Join(binDir, "mcp-broker-server")
	if err := os.WriteFile(mcpBrokerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp broker: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "api-key-should-not-be-forwarded")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "oauth-env-should-not-be-forwarded")

	mcpConfigDir := t.TempDir()
	mcpConfigPath := filepath.Join(mcpConfigDir, "mcp.json")
	if err := os.WriteFile(mcpConfigPath, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}

	outputStylePath, outputStyleCleanup, err := claudellm.WriteResearchOutputStyle()
	if err != nil {
		t.Fatalf("WriteResearchOutputStyle: %v", err)
	}
	defer outputStyleCleanup()

	cmd, cleanup, err := buildClaudePipelineSandboxCommand(
		context.Background(),
		"do the task",
		"system prompt",
		"sonnet",
		mcpConfigPath,
		mcpBrokerPath,
		claudePath,
		4.25,
		outputStylePath,
		nil,
	)
	if err != nil {
		t.Fatalf("buildClaudePipelineSandboxCommand returned error: %v", err)
	}
	defer cleanup()

	if !strings.Contains(filepath.Base(cmd.Dir), "gowild-claude-runner-") {
		t.Fatalf("cmd.Dir = %q, want scratch runner dir", cmd.Dir)
	}
	if !containsArgPair(cmd.Args, "--mcp-config", claudeCodeSandboxMCPConfig) {
		t.Fatalf("command args missing sandbox mcp config path: %v", cmd.Args)
	}
	if !containsArgPair(cmd.Args, "--disallowedTools", strings.Join(claudeCodeRunnerDisallowedTools, ",")) {
		t.Fatalf("command args missing disallowed tools: %v", cmd.Args)
	}
	if !containsArgPair(cmd.Args, "--output-format", "stream-json") {
		t.Fatalf("command args missing stream-json output format: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--verbose") {
		t.Fatalf("command args missing verbose flag: %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--strict-mcp-config") {
		t.Fatalf("command args missing strict MCP config: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--ro-bind", mustAbsPath(t, mcpConfigPath), claudeCodeSandboxMCPConfig) {
		t.Fatalf("command args missing bound MCP config: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--ro-bind", mustAbsPath(t, mcpBrokerPath), claudeCodeSandboxMCPBroker) {
		t.Fatalf("command args missing bound MCP broker binary: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--bind", cmd.Dir, "/work") {
		t.Fatalf("command args missing scratch workdir bind: %v", cmd.Args)
	}
	if !containsArgTriplet(cmd.Args, "--ro-bind", mustAbsPath(t, outputStylePath), claudellm.SandboxOutputStylePath) {
		t.Fatalf("command args missing bound output style: %v", cmd.Args)
	}
	if !containsArgPair(cmd.Args, "--settings", fmt.Sprintf(`{"outputStyle":"%s"}`, claudellm.SandboxOutputStylePath)) {
		t.Fatalf("command args missing output style settings: %v", cmd.Args)
	}
	if containsArg(cmd.Args, "ANTHROPIC_API_KEY") || containsArg(cmd.Args, "ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("command args should not forward auth env vars: %v", cmd.Args)
	}

	scratchDir := cmd.Dir
	cleanup()
	if _, err := os.Stat(scratchDir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir should be removed after cleanup, stat err=%v", err)
	}
}

func TestParseClaudeCodeResultUnwrapsStructuredResult(t *testing.T) {
	output := `{
  "type": "result",
  "result": "{\"status\":\"succeeded\",\"result\":{\"condition_id\":\"cond-1\",\"question\":\"Q?\",\"confidence\":0.77}}"
}`

	parsed := claudellm.ParseResult(output)
	if parsed.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", parsed.Status)
	}
	if parsed.Payload["condition_id"] != "cond-1" {
		t.Fatalf("payload.condition_id = %#v", parsed.Payload["condition_id"])
	}
	if parsed.Payload["question"] != "Q?" {
		t.Fatalf("payload.question = %#v", parsed.Payload["question"])
	}
}

func TestParseClaudeCodeResultExtractsJSONAfterNarrative(t *testing.T) {
	output := "{\n" +
		"  \"type\": \"result\",\n" +
		"  \"result\": \"I investigated the market.\\n\\n```json\\n{\\\"status\\\":\\\"succeeded\\\",\\\"result\\\":{\\\"condition_id\\\":\\\"cond-2\\\",\\\"tokens\\\":[{\\\"outcome\\\":\\\"Yes\\\",\\\"token_id\\\":\\\"tok-yes\\\"},{\\\"outcome\\\":\\\"No\\\",\\\"token_id\\\":\\\"tok-no\\\"}]}}\\n```\"\n" +
		"}"

	parsed := claudellm.ParseResult(output)
	if parsed.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", parsed.Status)
	}
	if parsed.Payload["condition_id"] != "cond-2" {
		t.Fatalf("payload.condition_id = %#v", parsed.Payload["condition_id"])
	}
	tokens, ok := parsed.Payload["tokens"].([]any)
	if !ok || len(tokens) != 2 {
		t.Fatalf("payload.tokens = %#v", parsed.Payload["tokens"])
	}
}

func TestParseClaudeCodeResultPreservesFailureStatus(t *testing.T) {
	output := `{
  "type": "result",
  "result": "{\"status\":\"failed\",\"reason\":\"research was inconclusive\",\"result\":{\"question\":\"Q?\"}}"
}`

	parsed := claudellm.ParseResult(output)
	if parsed.Status != "failed" {
		t.Fatalf("status = %q, want failed", parsed.Status)
	}
	if parsed.FailureReason != "research was inconclusive" {
		t.Fatalf("failure reason = %q", parsed.FailureReason)
	}
	if parsed.Payload["question"] != "Q?" {
		t.Fatalf("payload.question = %#v", parsed.Payload["question"])
	}
}

func TestParseClaudeCodeResultRejectsPlainTextOutput(t *testing.T) {
	parsed := claudellm.ParseResult("I could not produce valid JSON.")
	if parsed.Status != "failed" {
		t.Fatalf("status = %q, want failed", parsed.Status)
	}
	if !strings.Contains(parsed.FailureReason, "non-JSON") {
		t.Fatalf("failure reason = %q, want non-JSON error", parsed.FailureReason)
	}
}

func TestParseClaudeCodeResultRejectsNonEnvelopeJSON(t *testing.T) {
	parsed := claudellm.ParseResult(`{"condition_id":"cond-9","decision":"buy"}`)
	if parsed.Status != "failed" {
		t.Fatalf("status = %q, want failed", parsed.Status)
	}
	if !strings.Contains(parsed.FailureReason, "required status field") {
		t.Fatalf("failure reason = %q, want missing status error", parsed.FailureReason)
	}
}

func TestParseClaudeCodeResultExtractsFromStreamJSON(t *testing.T) {
	output := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-sonnet","cwd":"/work"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"polymarket_get_market","input":{"condition_id":"cond-3"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"{\"condition_id\":\"cond-3\",\"liquidity\":12345}","is_error":false}]}}`,
		`{"type":"result","subtype":"success","result":"{\"status\":\"succeeded\",\"result\":{\"condition_id\":\"cond-3\",\"decision\":\"hold\"}}","stop_reason":"end_turn"}`,
	}, "\n")

	parsed := claudellm.ParseResult(output)
	if parsed.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", parsed.Status)
	}
	if parsed.Payload["condition_id"] != "cond-3" {
		t.Fatalf("payload.condition_id = %#v", parsed.Payload["condition_id"])
	}
	if parsed.Payload["decision"] != "hold" {
		t.Fatalf("payload.decision = %#v", parsed.Payload["decision"])
	}
}

func TestFormatClaudeCodeEventLogIncludesToolCalls(t *testing.T) {
	output := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-2","model":"claude-sonnet","cwd":"/work"}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hidden"},{"type":"tool_use","id":"toolu_2","name":"polymarket_check_policy","input":{"condition_id":"cond-4"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"{\"checked\":true}","is_error":false}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"{\"status\":\"succeeded\",\"result\":{\"condition_id\":\"cond-4\"}}"}]}}`,
		`{"type":"result","subtype":"success","result":"{\"status\":\"succeeded\",\"result\":{\"condition_id\":\"cond-4\"}}","duration_ms":1234}`,
	}, "\n")

	logText := claudellm.FormatEventLog(output)
	if !strings.Contains(logText, "ASSISTANT tool_use: polymarket_check_policy") {
		t.Fatalf("event log missing tool_use: %s", logText)
	}
	if !strings.Contains(logText, "USER tool_result") {
		t.Fatalf("event log missing tool_result: %s", logText)
	}
	if !strings.Contains(logText, `"condition_id": "cond-4"`) {
		t.Fatalf("event log missing condition_id: %s", logText)
	}
	if strings.Contains(logText, "hidden") {
		t.Fatalf("event log should not include thinking content: %s", logText)
	}
}

func TestExecuteClaudeCodeStepPersistsRunningStateBeforeCompletion(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_running_state",
		Name: "Claude Running State",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "inspect_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-running-state",
		PipelineID:   "claude_running_state",
		TriggerJobID: "manual-seed",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	sawRunningState := false
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		if agentID != targetAgent.ID {
			t.Fatalf("runner agentID = %q, want %q", agentID, targetAgent.ID)
		}

		stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("ListStepRunsForRun failed: %v", err)
		}
		if len(stepRuns) != 1 {
			t.Fatalf("expected 1 running step, got %d", len(stepRuns))
		}
		if stepRuns[0].Status != "running" {
			t.Fatalf("step status = %q, want running", stepRuns[0].Status)
		}
		if !stepRuns[0].CompletedAt.IsZero() {
			t.Fatalf("running step should not have completed_at set")
		}

		job, err := newLocalA2AQueue(db).GetJob(ctx, stepRuns[0].A2AJobID)
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if got, _ := job["status"].(string); got != "running" {
			t.Fatalf("job status = %q, want running", got)
		}

		detail, err := engine.GetPipelineRunDetailEnriched(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetPipelineRunDetailEnriched failed: %v", err)
		}
		steps, ok := detail["steps"].([]enrichedStepRun)
		if !ok {
			t.Fatalf("steps type = %T", detail["steps"])
		}
		if len(steps) != 1 {
			t.Fatalf("expected 1 enriched step, got %d", len(steps))
		}
		if steps[0].Status != "running" {
			t.Fatalf("enriched step status = %q, want running", steps[0].Status)
		}
		request, ok := steps[0].Request.(map[string]any)
		if !ok {
			t.Fatalf("request type = %T", steps[0].Request)
		}
		params, ok := request["params"].(map[string]any)
		if !ok || params["foo"] != "bar" {
			t.Fatalf("request params = %#v", request["params"])
		}

		storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetPipelineRun failed: %v", err)
		}
		if storedRun.Status != "running" {
			t.Fatalf("run status = %q, want running while claude step executes", storedRun.Status)
		}

		engine.processCompletions(ctx)

		stepRunsAfterPoll, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("ListStepRunsForRun after poll failed: %v", err)
		}
		if len(stepRunsAfterPoll) != 1 {
			t.Fatalf("expected 1 running step after poll, got %d", len(stepRunsAfterPoll))
		}
		if stepRunsAfterPoll[0].Status != "running" {
			t.Fatalf("step status after poll = %q, want running", stepRunsAfterPoll[0].Status)
		}

		sawRunningState = true
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	if ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "inspect_market",
	}, 0, nil, map[string]any{"foo": "bar"}); !ok {
		t.Fatalf("executeClaudeCodeStep returned false")
	}
	if !sawRunningState {
		t.Fatalf("runner callback did not observe running state")
	}

	stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListStepRunsForRun final failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 final step run, got %d", len(stepRuns))
	}
	if stepRuns[0].Status != "succeeded" {
		t.Fatalf("final step status = %q, want succeeded", stepRuns[0].Status)
	}
	if stepRuns[0].CompletedAt.IsZero() {
		t.Fatalf("final step completed_at should be set")
	}

	finalRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun final failed: %v", err)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("final run status = %q, want completed", finalRun.Status)
	}
}

func TestExecuteClaudeCodeStepPromptIncludesMethodMetadataAndRedactsPriorContext(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-metadata-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(
		ctx,
		"inspect_market",
		"research market moves",
		"Use primary sources and include a decision field.",
		`{"type":"object","properties":{"condition_id":{"type":"string"}},"required":["condition_id"]}`,
		`{"type":"object","properties":{"decision":{"type":"string"}},"required":["decision"]}`,
		false,
		false,
		true,
		false,
		false,
	); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_prompt_metadata",
		Name: "Claude Prompt Metadata",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "inspect_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	run := &data.PipelineRun{
		ID:           "run-claude-prompt-metadata",
		PipelineID:   "claude_prompt_metadata",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	sawPrompt := false
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		if !strings.Contains(prompt, "research market moves") {
			t.Fatalf("prompt missing method description: %s", prompt)
		}
		if !strings.Contains(prompt, "Use primary sources and include a decision field.") {
			t.Fatalf("prompt missing method instructions: %s", prompt)
		}
		if !strings.Contains(prompt, "\"condition_id\"") {
			t.Fatalf("prompt missing input schema: %s", prompt)
		}
		if !strings.Contains(prompt, "\"decision\"") {
			t.Fatalf("prompt missing output schema: %s", prompt)
		}
		if !strings.Contains(prompt, "Context from Prior Step") {
			t.Fatalf("prompt missing prior-step context: %s", prompt)
		}
		if strings.Contains(prompt, "best_bid") {
			t.Fatalf("prompt leaked best_bid into prior-step context: %s", prompt)
		}
		if strings.Contains(prompt, "curPrice") {
			t.Fatalf("prompt leaked curPrice into prior-step context: %s", prompt)
		}
		if strings.Contains(prompt, "currentValue") {
			t.Fatalf("prompt leaked currentValue into prior-step context: %s", prompt)
		}
		sawPrompt = true
		return `{"status":"succeeded","result":{"decision":"hold"}}`, nil
	}

	if ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "inspect_market",
	}, 0, map[string]any{
		"condition_id": "cond-11",
		"best_bid":     0.44,
		"position": map[string]any{
			"curPrice":     0.44,
			"currentValue": 99.0,
			"avgPrice":     0.31,
		},
	}, map[string]any{
		"condition_id": "cond-11",
	}); !ok {
		t.Fatalf("executeClaudeCodeStep returned false")
	}
	if !sawPrompt {
		t.Fatalf("runner did not observe claude mission prompt")
	}
}

func TestExecuteClaudeCodeStepPersistsFailureArtifacts(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-failure-artifacts-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_failure_artifacts",
		Name: "Claude Failure Artifacts",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "inspect_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-failure-artifacts",
		PipelineID:   "claude_failure_artifacts",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	streamOutput := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-debug","model":"claude-sonnet","cwd":"/work"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_debug","name":"polymarket_get_market","input":{"condition_id":"cond-debug"}}]}}`,
	}, "\n")
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		return streamOutput, &claudellm.ExecutionError{
			Message:  "claude-code failed (exit 1): permission denied",
			ExitCode: 1,
			Stdout:   streamOutput,
			Stderr:   "permission denied",
		}
	}

	if ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "inspect_market",
	}, 0, nil, map[string]any{"condition_id": "cond-debug"}); ok {
		t.Fatalf("executeClaudeCodeStep returned true")
	}

	stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListStepRunsForRun failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}
	job, err := newLocalA2AQueue(db).GetJob(ctx, stepRuns[0].A2AJobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	result, _ := job["result"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(result["raw_output"])) != streamOutput {
		t.Fatalf("result.raw_output = %#v", result["raw_output"])
	}
	if strings.TrimSpace(fmt.Sprint(result["stderr"])) != "permission denied" {
		t.Fatalf("result.stderr = %#v", result["stderr"])
	}
	if got, _ := result["exit_code"].(float64); got != 1 {
		t.Fatalf("result.exit_code = %#v, want 1", result["exit_code"])
	}
	if !strings.Contains(fmt.Sprint(result["event_log"]), "polymarket_get_market") {
		t.Fatalf("result.event_log = %#v", result["event_log"])
	}
	errPayload, _ := job["error"].(map[string]any)
	if strings.TrimSpace(fmt.Sprint(errPayload["stderr"])) != "permission denied" {
		t.Fatalf("error.stderr = %#v", errPayload["stderr"])
	}
	if strings.TrimSpace(fmt.Sprint(errPayload["stdout"])) != streamOutput {
		t.Fatalf("error.stdout = %#v", errPayload["stdout"])
	}

	detail, err := engine.GetPipelineRunDetailEnriched(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRunDetailEnriched failed: %v", err)
	}
	steps, ok := detail["steps"].([]enrichedStepRun)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %#v", detail["steps"])
	}
	if steps[0].ClaudeStderr != "permission denied" {
		t.Fatalf("claude_stderr = %q", steps[0].ClaudeStderr)
	}
	if steps[0].RawOutput != streamOutput {
		t.Fatalf("raw_output = %q", steps[0].RawOutput)
	}
	if !strings.Contains(steps[0].ClaudeLog, "polymarket_get_market") {
		t.Fatalf("claude_log = %q", steps[0].ClaudeLog)
	}
}

func TestProcessCompletionsFailsOrphanedClaudeCodeJobAfterRestart(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-recovery-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_restart_recovery",
		Name: "Claude Restart Recovery",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "inspect_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	// Use a timestamp far enough in the past to exceed the orphan grace period
	// (pipelineRunnerMaxTimeoutSec + 600s buffer).
	staleStart := time.Now().UTC().Add(-time.Duration(pipelineRunnerMaxTimeoutSec+700) * time.Second)
	run := &data.PipelineRun{
		ID:           "run-claude-restart-recovery",
		PipelineID:   "claude_restart_recovery",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    staleStart,
		UpdatedAt:    staleStart,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	jobID := "claude-code-recovery-job"
	engine.recordPipelineJob(ctx, jobID, targetAgent.ID, localA2ARequest{
		Method: "inspect_market",
		Params: map[string]any{"condition_id": "cond-12"},
	}, "running", nil, nil, time.Time{})

	stepRun := &data.PipelineStepRun{
		ID:        "claude-step-recovery",
		RunID:     run.ID,
		StepIndex: 0,
		A2AJobID:  jobID,
		Status:    "running",
		StartedAt: staleStart,
	}
	if err := systemSvc.CreateStepRun(ctx, stepRun); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	engine.processCompletions(ctx)

	var storedStep data.PipelineStepRun
	if err := db.Table(data.PipelineStepRun{}).Get(ctx, stepRun.ID, &storedStep); err != nil {
		t.Fatalf("Get step run failed: %v", err)
	}
	if storedStep.Status != "failed" {
		t.Fatalf("step status = %q, want failed", storedStep.Status)
	}
	if storedStep.CompletedAt.IsZero() {
		t.Fatalf("expected completed_at to be set")
	}

	job, err := newLocalA2AQueue(db).GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if got, _ := job["status"].(string); got != "failed" {
		t.Fatalf("job status = %q, want failed", got)
	}
	errPayload, _ := job["error"].(map[string]any)
	message, _ := errPayload["message"].(string)
	if !strings.Contains(message, "cannot be resumed after manager restart") {
		t.Fatalf("job error message = %q", message)
	}

	storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if storedRun.Status != "failed" {
		t.Fatalf("run status = %q, want failed", storedRun.Status)
	}
	if !strings.Contains(storedRun.FailureReason, "cannot be resumed after manager restart") {
		t.Fatalf("run failure reason = %q", storedRun.FailureReason)
	}
}

func TestExecuteClaudeCodeStepCorrectionOnMalformedOutput(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-correction-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_correction",
		Name: "Claude Correction",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-correction",
		PipelineID:   "claude_correction",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	callCount := 0
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		callCount++
		if callCount == 1 {
			// First call: return malformed output (plain text, not JSON envelope)
			if disabled, _ := ctx.Value(pipelineDisableAllToolsKey).(bool); disabled {
				t.Fatalf("original run should not disable all tools")
			}
			return `The policy check passed with no violations.`, nil
		}
		// Second call (correction): verify it's a correction prompt and return proper JSON
		if disabled, _ := ctx.Value(pipelineDisableAllToolsKey).(bool); !disabled {
			t.Fatalf("correction run should disable all tools")
		}
		if !strings.Contains(prompt, "did not meet the required output format") {
			t.Fatalf("correction prompt missing format error message: %s", prompt)
		}
		if !strings.Contains(prompt, "The policy check passed") {
			t.Fatalf("correction prompt missing original response: %s", prompt)
		}
		if !strings.Contains(prompt, "do not call tools") {
			t.Fatalf("correction prompt missing no-tools instruction: %s", prompt)
		}
		if systemPrompt != "" {
			t.Fatalf("correction systemPrompt = %q, want empty", systemPrompt)
		}
		if budgetUSD != claudeCodeRunnerCorrectionBudgetUSD {
			t.Fatalf("correction budget = %.2f, want %.2f", budgetUSD, claudeCodeRunnerCorrectionBudgetUSD)
		}
		return `{"status":"succeeded","result":{"violations":[],"checked":true}}`, nil
	}

	ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if !ok {
		t.Fatalf("executeClaudeCodeStep returned false, expected success after correction")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 invocations (original + correction), got %d", callCount)
	}

	storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if storedRun.Status == "failed" {
		t.Fatalf("run status = %q, should not be failed after successful correction", storedRun.Status)
	}
}

func TestBuildClaudePipelineSandboxCommandDisablesBuiltInToolsWhenRequested(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	claudeDir := filepath.Join(userHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), []byte(`{"token":"personal-oauth"}`), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	binDir := t.TempDir()
	bwrapPath := filepath.Join(binDir, "bwrap")
	if err := os.WriteFile(bwrapPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}
	claudePath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	mcpBrokerPath := filepath.Join(binDir, "mcp-broker-server")
	if err := os.WriteFile(mcpBrokerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake mcp broker: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	mcpConfigDir := t.TempDir()
	mcpConfigPath := filepath.Join(mcpConfigDir, "mcp.json")
	if err := os.WriteFile(mcpConfigPath, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}

	outputStylePath, outputStyleCleanup, err := claudellm.WriteResearchOutputStyle()
	if err != nil {
		t.Fatalf("WriteResearchOutputStyle: %v", err)
	}
	defer outputStyleCleanup()

	cmd, cleanup, err := buildClaudePipelineSandboxCommand(
		context.Background(),
		"reformat this",
		"",
		"sonnet",
		mcpConfigPath,
		mcpBrokerPath,
		claudePath,
		0.50,
		outputStylePath,
		[]string{},
	)
	if err != nil {
		t.Fatalf("buildClaudePipelineSandboxCommand returned error: %v", err)
	}
	defer cleanup()

	if !containsArgPair(cmd.Args, "--tools", "") {
		t.Fatalf("command args missing empty --tools flag: %v", cmd.Args)
	}
}

func TestExecuteClaudeCodeStepCorrectionDoesNotLoop(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-noloop-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_noloop",
		Name: "Claude No Loop",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-noloop",
		PipelineID:   "claude_noloop",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	callCount := 0
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		callCount++
		// Both calls return malformed output — should not retry more than once.
		return `Still not JSON.`, nil
	}

	ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if ok {
		t.Fatalf("executeClaudeCodeStep returned true, expected failure")
	}
	if callCount != 2 {
		t.Fatalf("expected exactly 2 invocations (original + one correction attempt), got %d", callCount)
	}

	storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if storedRun.Status != "failed" {
		t.Fatalf("run status = %q, want failed", storedRun.Status)
	}
}

func TestExecuteClaudeCodeStepNoCorrectionOnExplicitFailure(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-noretryfail-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_noretryfail",
		Name: "Claude No Retry Fail",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-noretryfail",
		PipelineID:   "claude_noretryfail",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	callCount := 0
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		callCount++
		// LLM explicitly returns {"status":"failed"} — no correction should be attempted.
		return `{"status":"failed","reason":"policy violation detected"}`, nil
	}

	ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if ok {
		t.Fatalf("executeClaudeCodeStep returned true, expected failure")
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 invocation (no correction for explicit failure), got %d", callCount)
	}
}

// TestExecuteClaudeCodeStepReportsMissingModelEnvAsCleanFailure pins the
// panic→error conversion for resolveClaudeModel. Pipeline steps run in a
// detached goroutine (see TriggerPipeline), so a panic here would crash
// the whole manager — the error must instead surface as a clean
// step/run failure with the specific env var named.
func TestExecuteClaudeCodeStepReportsMissingModelEnvAsCleanFailure(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "")
	t.Setenv("CLAUDE_FAST_MODEL", "")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "claude-missing-env-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_missing_env",
		Name: "Claude Missing Env",
		Steps: []PipelineStep{{
			Runner:     pipelineStepRunnerClaudeCode,
			OnMethod:   "seed",
			ToAgentID:  targetAgent.ID,
			NextMethod: "check_policy",
		}},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-claude-missing-env",
		PipelineID:   "claude_missing_env",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	runnerCalled := false
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		runnerCalled = true
		return "", nil
	}

	// The whole point: this must return false, not panic.
	ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if ok {
		t.Fatalf("executeClaudeCodeStep returned true, expected failure")
	}
	if runnerCalled {
		t.Fatalf("claude runner should not be invoked when CLAUDE_SMART_MODEL is unset")
	}

	storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if storedRun.Status != "failed" {
		t.Fatalf("run status = %q, want failed", storedRun.Status)
	}
	if !strings.Contains(storedRun.FailureReason, "CLAUDE_SMART_MODEL") {
		t.Fatalf("run failure reason %q should name the missing env var", storedRun.FailureReason)
	}
}

// TestExecuteCodexStepReportsMissingProfileEnvAsCleanFailure mirrors the
// claude regression for the codex path. Same goroutine-crash risk applies:
// pipeline steps run detached from the HTTP request, so a panic in
// resolveCodexProfile would crash the manager process.
func TestExecuteCodexStepReportsMissingProfileEnvAsCleanFailure(t *testing.T) {
	t.Setenv("CODEX_SMART_PROFILE", "")
	t.Setenv("CODEX_FAST_PROFILE", "")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "codex-missing-env-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "codex_missing_env",
		Name: "Codex Missing Env",
		Steps: []PipelineStep{{
			Runner:     pipelineStepRunnerCodex,
			OnMethod:   "seed",
			ToAgentID:  targetAgent.ID,
			NextMethod: "check_policy",
		}},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-codex-missing-env",
		PipelineID:   "codex_missing_env",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	runnerCalled := false
	engine.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
		runnerCalled = true
		return "", nil
	}

	// The whole point: this must return false, not panic.
	ok := engine.executeCodexStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerCodex,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if ok {
		t.Fatalf("executeCodexStep returned true, expected failure")
	}
	if runnerCalled {
		t.Fatalf("codex runner should not be invoked when CODEX_SMART_PROFILE is unset")
	}

	storedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if storedRun.Status != "failed" {
		t.Fatalf("run status = %q, want failed", storedRun.Status)
	}
	if !strings.Contains(storedRun.FailureReason, "CODEX_SMART_PROFILE") {
		t.Fatalf("run failure reason %q should name the missing env var", storedRun.FailureReason)
	}
}

// TestExecuteCodexStepRunsWhenProfileEnvSet exercises the happy path so
// codexRunnerSpec's invoke closure is covered with profileErr=nil. Without
// this, the codex path has zero coverage and a regression to a hardcoded
// profile or an ignored profile flag would slip through.
func TestExecuteCodexStepRunsWhenProfileEnvSet(t *testing.T) {
	t.Setenv("CODEX_SMART_PROFILE", "smart-profile-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")

	targetAgent, err := svc.CreateAgent(ctx, "codex-happy-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "codex_happy",
		Name: "Codex Happy",
		Steps: []PipelineStep{{
			Runner:     pipelineStepRunnerCodex,
			OnMethod:   "seed",
			ToAgentID:  targetAgent.ID,
			NextMethod: "check_policy",
		}},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	run := &data.PipelineRun{
		ID:           "run-codex-happy",
		PipelineID:   "codex_happy",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	var gotProfile string
	engine.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
		gotProfile = profile
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	ok := engine.executeCodexStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerCodex,
		ToAgentID:  targetAgent.ID,
		NextMethod: "check_policy",
	}, 0, nil, map[string]any{"condition_id": "cond-1"})
	if !ok {
		t.Fatalf("executeCodexStep returned false, expected success")
	}
	if gotProfile != "smart-profile-test" {
		t.Fatalf("codex runner received profile %q, want smart-profile-test", gotProfile)
	}
}

// TestResolveCodexProfileFastTier pins the tiered profile dispatch —
// model_tier="fast" must select CODEX_FAST_PROFILE, not CODEX_SMART_PROFILE.
// A symmetric claude-side check is implicit in the TestResolveTieredEnvConfig
// helper tests, but the codex wrapper itself had no direct coverage.
func TestResolveCodexProfileFastTier(t *testing.T) {
	t.Setenv("CODEX_FAST_PROFILE", "fast-profile-test")
	t.Setenv("CODEX_SMART_PROFILE", "smart-profile-test")

	pe := &PipelineEngine{}
	got, err := pe.resolveCodexProfile(PipelineStep{}, &data.A2AMethod{ModelTier: "fast"})
	if err != nil {
		t.Fatalf("resolveCodexProfile returned error: %v", err)
	}
	if got != "fast-profile-test" {
		t.Fatalf("resolveCodexProfile = %q, want fast-profile-test", got)
	}

	// nil methodDef → smart path.
	got, err = pe.resolveCodexProfile(PipelineStep{}, nil)
	if err != nil {
		t.Fatalf("resolveCodexProfile(nil) returned error: %v", err)
	}
	if got != "smart-profile-test" {
		t.Fatalf("resolveCodexProfile(nil) = %q, want smart-profile-test", got)
	}
}

// TestInvokeCodexRunnerOnAcquireOrdering pins invokeCodexRunner's internal
// contract after the variadic-to-single-callback simplification: onAcquire
// (when non-nil) runs before the runner in the same goroutine, nil onAcquire
// is tolerated without deref panic, and the runner's return values propagate
// verbatim. The ordering guarantee is load-bearing for the codex
// deferFanOutActivation path — executePipelineStepShared passes
// activateStepRun as onAcquire so the step-run transitions to "running"
// before real work begins.
func TestInvokeCodexRunnerOnAcquireOrdering(t *testing.T) {
	t.Setenv("CODEX_SMART_PROFILE", "smart-profile-test")

	t.Run("non-nil onAcquire runs before runner and in the same goroutine", func(t *testing.T) {
		pe := &PipelineEngine{}
		var events []string
		pe.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
			events = append(events, "runner:"+agentID+":"+profile)
			return "runner-output", nil
		}

		activateCalled := 0
		out, err := pe.invokeCodexRunner(context.Background(), "agent-x", "prompt-x", "sys-x", "smart-profile-test", func() {
			activateCalled++
			events = append(events, "activate")
		})
		if err != nil {
			t.Fatalf("invokeCodexRunner returned error: %v", err)
		}
		if out != "runner-output" {
			t.Fatalf("invokeCodexRunner output = %q, want runner-output", out)
		}
		if activateCalled != 1 {
			t.Fatalf("activate called %d times, want 1", activateCalled)
		}
		// Ordering: activate must run strictly before the runner, both in the
		// same goroutine. Appending to `events` without a mutex only works
		// because invoke is sequential; if that ever regresses (e.g. someone
		// spawns onAcquire in a goroutine), -race will flag this test.
		if len(events) != 2 || events[0] != "activate" || events[1] != "runner:agent-x:smart-profile-test" {
			t.Fatalf("event ordering = %v, want [activate, runner:agent-x:smart-profile-test]", events)
		}
	})

	t.Run("nil onAcquire is tolerated without deref panic", func(t *testing.T) {
		pe := &PipelineEngine{}
		runnerCalled := false
		pe.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
			runnerCalled = true
			return "correction-output", nil
		}
		out, err := pe.invokeCodexRunner(context.Background(), "agent-y", "prompt-y", "sys-y", "smart-profile-test", nil)
		if err != nil {
			t.Fatalf("invokeCodexRunner(nil onAcquire) returned error: %v", err)
		}
		if !runnerCalled {
			t.Fatalf("runner was not called when onAcquire was nil")
		}
		if out != "correction-output" {
			t.Fatalf("invokeCodexRunner output = %q, want correction-output", out)
		}
	})

	t.Run("runner return values propagate verbatim", func(t *testing.T) {
		// Pins that a runner error — not just success — passes through
		// invokeCodexRunner unchanged, including partial stdout. The shared
		// executor's error branch (pipeline_runner_shared.go) inspects the
		// returned (output, err) pair to build the failure artifact, so the
		// propagation contract is what matters here — not whether activate
		// fires on the error path. (On the deferred fan-out error path
		// specifically, stepRunPersisted is already true from pre-creation,
		// so the executor can transition queued→failed without activate
		// having run at all.)
		pe := &PipelineEngine{}
		sentinel := fmt.Errorf("codex boom")
		pe.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
			return "partial-stdout", sentinel
		}
		out, err := pe.invokeCodexRunner(context.Background(), "agent-z", "p", "s", "smart-profile-test", func() {})
		if err != sentinel {
			t.Fatalf("err = %v, want sentinel %v", err, sentinel)
		}
		if out != "partial-stdout" {
			t.Fatalf("out = %q, want partial-stdout", out)
		}
	})
}

// TestExecutePipelineStepPropagatesDisabledToolsKey pins the write site of
// pipelineDisabledToolsKey in executePipelineStepShared: given a method with
// DisabledToolGroups=[wallet], both runner-dispatch paths (claude-code and
// codex) must surface the expanded tool names in the context passed to the
// runner's invoke callback, under the typed pipelineDisabledToolsKey
// constant. Two subtests cover executeClaudeCodeStep and executeCodexStep
// respectively so the shared executor's write contract is pinned for both
// runner-spec invocations.
//
// Scope — what this test does and does NOT cover:
//   - It DOES cover the writer at pipeline_runner_shared.go:276, where the
//     context key is set from methodDef.DisabledToolNames(). Mutation-tested:
//     swapping the write-site constant to contextKey("wrong_key") fails both
//     subtests cleanly with "got []".
//   - It does NOT cover the production readers at runClaudeCode:126 and
//     runCodex:142. Both invokeClaudeCodeRunner and invokeCodexRunner
//     short-circuit to the test seams (engine.claudeCodeRunner /
//     engine.codexRunner) before reaching those CLI functions, which spawn
//     bwrap+claude/codex binaries and cannot run in unit tests. Reader-side
//     breakage from a partial rename is instead caught at compile time: the
//     old typed constants claudeCodeDisabledToolsKey /
//     claudeCodeDisableAllToolsKey are deleted, so any stale reader would
//     fail with UndeclaredName rather than silently miss the lookup. A reader
//     that used the raw string literal "claude_code_disabled_tools" (bypassing
//     the typed constant entirely) would not be caught by this test or the
//     compiler — but no such caller exists, and introducing one would require
//     an explicit choice to drop the typed constant.
//
// The sibling pipelineDisableAllToolsKey is already covered end-to-end by
// TestExecuteClaudeCodeStepCorrectionOnMalformedOutput, which asserts both
// branches of the correction-retry gate through the claude-code seam. This
// test fills the gap for the write site of the other half of the pair.
func TestExecutePipelineStepPropagatesDisabledToolsKey(t *testing.T) {
	t.Run("claude-code runner receives disabled tool names via context", func(t *testing.T) {
		t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

		ctx := context.Background()
		db := setupManagerTestDB(t)
		svc := NewAgentService(db)
		engine := NewPipelineEngine(db, svc)
		systemSvc := data.NewAgentService(db, "system")

		targetAgent, err := svc.CreateAgent(ctx, "claude-disabled-tools-target")
		if err != nil {
			t.Fatalf("CreateAgent failed: %v", err)
		}
		// wallet group expands to 11 tool names including get_wallet_address
		// and send_token — see agent_data/tool_groups.go.
		if _, err := systemSvc.CreateA2AMethodWithConfig(
			ctx,
			"check_policy",
			"policy check",
			"",
			`{"type":"object"}`,
			`{"type":"object"}`,
			false, false, false, false, false,
			data.WithDisabledToolGroups([]string{"wallet"}),
		); err != nil {
			t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
		}
		if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
			ID:   "claude_disabled_tools",
			Name: "Claude Disabled Tools",
			Steps: []PipelineStep{{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			}},
		}, true); err != nil {
			t.Fatalf("UpsertPipelineDefinition failed: %v", err)
		}

		now := time.Now().UTC()
		run := &data.PipelineRun{
			ID:           "run-claude-disabled-tools",
			PipelineID:   "claude_disabled_tools",
			TriggerJobID: "manual",
			CurrentStep:  0,
			Status:       "running",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
			t.Fatalf("CreatePipelineRun failed: %v", err)
		}

		var gotDisabled []string
		var gotDisableAll bool
		engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
			gotDisabled, _ = ctx.Value(pipelineDisabledToolsKey).([]string)
			gotDisableAll, _ = ctx.Value(pipelineDisableAllToolsKey).(bool)
			return `{"status":"succeeded","result":{"ok":true}}`, nil
		}

		ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
			Runner:     pipelineStepRunnerClaudeCode,
			ToAgentID:  targetAgent.ID,
			NextMethod: "check_policy",
		}, 0, nil, map[string]any{"condition_id": "cond-1"})
		if !ok {
			t.Fatalf("executeClaudeCodeStep returned false")
		}
		if gotDisableAll {
			t.Fatalf("original run should not carry pipelineDisableAllToolsKey=true")
		}
		if !containsString(gotDisabled, "send_token") || !containsString(gotDisabled, "get_wallet_address") {
			t.Fatalf("disabled tool names did not propagate through pipelineDisabledToolsKey: got %v", gotDisabled)
		}
	})

	t.Run("codex runner receives disabled tool names via context", func(t *testing.T) {
		t.Setenv("CODEX_SMART_PROFILE", "smart-profile-test")

		ctx := context.Background()
		db := setupManagerTestDB(t)
		svc := NewAgentService(db)
		engine := NewPipelineEngine(db, svc)
		systemSvc := data.NewAgentService(db, "system")

		targetAgent, err := svc.CreateAgent(ctx, "codex-disabled-tools-target")
		if err != nil {
			t.Fatalf("CreateAgent failed: %v", err)
		}
		if _, err := systemSvc.CreateA2AMethodWithConfig(
			ctx,
			"check_policy",
			"policy check",
			"",
			`{"type":"object"}`,
			`{"type":"object"}`,
			false, false, false, false, false,
			data.WithDisabledToolGroups([]string{"wallet"}),
		); err != nil {
			t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
		}
		if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
			ID:   "codex_disabled_tools",
			Name: "Codex Disabled Tools",
			Steps: []PipelineStep{{
				Runner:     pipelineStepRunnerCodex,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			}},
		}, true); err != nil {
			t.Fatalf("UpsertPipelineDefinition failed: %v", err)
		}

		now := time.Now().UTC()
		run := &data.PipelineRun{
			ID:           "run-codex-disabled-tools",
			PipelineID:   "codex_disabled_tools",
			TriggerJobID: "manual",
			CurrentStep:  0,
			Status:       "running",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
			t.Fatalf("CreatePipelineRun failed: %v", err)
		}

		var gotDisabled []string
		var gotDisableAll bool
		engine.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
			gotDisabled, _ = ctx.Value(pipelineDisabledToolsKey).([]string)
			gotDisableAll, _ = ctx.Value(pipelineDisableAllToolsKey).(bool)
			return `{"status":"succeeded","result":{"ok":true}}`, nil
		}

		ok := engine.executeCodexStep(ctx, systemSvc, run, PipelineStep{
			Runner:     pipelineStepRunnerCodex,
			ToAgentID:  targetAgent.ID,
			NextMethod: "check_policy",
		}, 0, nil, map[string]any{"condition_id": "cond-1"})
		if !ok {
			t.Fatalf("executeCodexStep returned false")
		}
		if gotDisableAll {
			t.Fatalf("original run should not carry pipelineDisableAllToolsKey=true")
		}
		if !containsString(gotDisabled, "send_token") || !containsString(gotDisabled, "get_wallet_address") {
			t.Fatalf("disabled tool names did not propagate through pipelineDisabledToolsKey: got %v", gotDisabled)
		}
	})
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

// TestFanOutCLIRunnerBranchHandlesEdgeCasesIdentically pins the invariant
// documented at pipelines_delivery.go:69-113: the claude-code and codex
// executeStep functions are interchangeable under CLI-runner fan-out because
// they both funnel through executePipelineStepShared, and the shared function
// is where the fan-out edge cases are handled. If someone pulls dispatch out
// of the shared path and re-implements either branch, these subtests break.
//
// The production path launches one goroutine per fan-out branch. This test
// exercises a single branch per runner at a time — the exact calling
// convention the fan-out goroutine body uses (explicitParams + a pre-created
// step-run in "queued" status) — to pin the per-branch invariants without
// the SQLite :memory: concurrency gotcha (each :memory: connection is its
// own independent DB, so goroutines that grab separate pool connections see
// empty schemas). Running the branches sequentially exercises the same
// executePipelineStepShared code the goroutines would.
//
// Scope — what this test does and does NOT cover:
//   - It DOES cover the shared-function err-path: any error returned from
//     invoke (regardless of source) must be treated identically by both
//     runner specs under fan-out (branch "failed", no cascade to
//     failPipelineRun, provider-specific spec.buildFailure type-assertion
//     exercised). The runner_error subtest injects the real
//     *claudellm.ExecutionError / *codexllm.ExecutionError that
//     runClaudeCode / runCodex construct on timeout, so spec.buildFailure's
//     provider-specific artifact shaping is exercised per runner.
//   - It does NOT cover the real context.WithTimeout / exec.CommandContext
//     plumbing inside runClaudeCode / runCodex — injected runners replace
//     those bodies, so the actual DeadlineExceeded translation, bwrap
//     lifecycle, and semaphore-acquire ordering aren't hit here. Those
//     are runner-implementation tests (covered by the existing build-sandbox
//     and semaphore-ordering tests), not fan-out-dispatch tests.
//   - Partial-failure cancellation-from-shutdown is covered by the
//     TestShutdown* family in pipelines_core_test.go, which exercises the
//     real runCtx/AfterFunc cancellation chain end-to-end.
func TestFanOutCLIRunnerBranchHandlesEdgeCasesIdentically(t *testing.T) {
	type runnerCase struct {
		name       string
		runnerKind string
		executeFn  func(pe *PipelineEngine) func(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result, explicit map[string]any, existing ...*data.PipelineStepRun) bool
		setup      func(t *testing.T, engine *PipelineEngine, handler func(ctx context.Context) (string, error))
		// newExecErr returns the provider-specific execution error that
		// runClaudeCode / runCodex emit when their internal
		// context.WithTimeout fires. Using the right concrete type per
		// runner is load-bearing: spec.buildFailure type-asserts on the
		// provider-specific *ExecutionError to shape the job result
		// payload, so a cross-type injection would silently fall through
		// to the generic err-path and hide divergence between the two
		// buildFailure implementations.
		newExecErr func(message, stderr string) error
	}
	cases := []runnerCase{
		{
			name:       "claude-code",
			runnerKind: pipelineStepRunnerClaudeCode,
			executeFn: func(pe *PipelineEngine) func(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result, explicit map[string]any, existing ...*data.PipelineStepRun) bool {
				return pe.executeClaudeCodeStep
			},
			setup: func(t *testing.T, engine *PipelineEngine, handler func(ctx context.Context) (string, error)) {
				t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")
				engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
					return handler(ctx)
				}
			},
			newExecErr: func(message, stderr string) error {
				return &claudellm.ExecutionError{Message: message, Stderr: stderr}
			},
		},
		{
			name:       "codex",
			runnerKind: pipelineStepRunnerCodex,
			executeFn: func(pe *PipelineEngine) func(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result, explicit map[string]any, existing ...*data.PipelineStepRun) bool {
				return pe.executeCodexStep
			},
			setup: func(t *testing.T, engine *PipelineEngine, handler func(ctx context.Context) (string, error)) {
				t.Setenv("CODEX_SMART_PROFILE", "smart-profile-test")
				engine.codexRunner = func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
					return handler(ctx)
				}
			},
			newExecErr: func(message, stderr string) error {
				return &codexllm.ExecutionError{Message: message, Stderr: stderr}
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name+"/partial_failure", func(t *testing.T) {
			ctx := context.Background()
			db := setupManagerTestDB(t)
			svc := NewAgentService(db)
			engine := NewPipelineEngine(db, svc)
			systemSvc := data.NewAgentService(db, "system")

			target, err := svc.CreateAgent(ctx, "fanout-partial-"+tc.name)
			if err != nil {
				t.Fatalf("CreateAgent failed: %v", err)
			}
			pipelineID := "fanout_partial_" + tc.name
			if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
				ID:   pipelineID,
				Name: "Fanout Partial " + tc.name,
				Steps: []PipelineStep{{
					Runner:     tc.runnerKind,
					OnMethod:   "seed",
					ToAgentID:  target.ID,
					NextMethod: "inspect_market",
					FanOut:     true,
					FanOutKey:  "items",
				}},
			}, true); err != nil {
				t.Fatalf("UpsertPipelineDefinition failed: %v", err)
			}

			now := time.Now().UTC()
			run := &data.PipelineRun{
				ID:           "run-fanout-partial-" + tc.name,
				PipelineID:   pipelineID,
				TriggerJobID: "manual",
				CurrentStep:  0,
				Status:       "running",
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
				t.Fatalf("CreatePipelineRun failed: %v", err)
			}

			// Pre-create a step-run in "queued" status, mirroring what
			// executeFanOutStep does before launching each goroutine.
			preCreated := &data.PipelineStepRun{
				ID:        "failing-branch-" + tc.name,
				RunID:     run.ID,
				StepIndex: 0,
				Status:    "queued",
			}
			if err := systemSvc.CreateStepRun(ctx, preCreated); err != nil {
				t.Fatalf("CreateStepRun failed: %v", err)
			}

			tc.setup(t, engine, func(ctx context.Context) (string, error) {
				return "", tc.newExecErr("simulated branch failure", "nope")
			})

			ok := tc.executeFn(engine)(ctx, systemSvc, run, PipelineStep{
				Runner:     tc.runnerKind,
				ToAgentID:  target.ID,
				NextMethod: "inspect_market",
				FanOut:     true,
				FanOutKey:  "items",
			}, 0, nil, map[string]any{"condition_id": "a"}, preCreated)

			if ok {
				t.Fatalf("%s fan-out branch returned true on injected runner error, want false", tc.name)
			}

			// Branch-level invariant: the step-run must be "failed".
			stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("ListStepRunsForRun failed: %v", err)
			}
			if len(stepRuns) != 1 {
				t.Fatalf("step run count = %d, want 1", len(stepRuns))
			}
			if stepRuns[0].Status != "failed" {
				t.Fatalf("failing branch status = %q, want failed", stepRuns[0].Status)
			}

			// Key invariant: even though the branch failed, the pipeline
			// run must stay "running" — failPipelineRun must NOT have
			// fired. Siblings in a real fan-out are free to keep running.
			// resolveRunStatus computes the verdict later, after wg.Wait.
			runAfterFailure, err := systemSvc.GetPipelineRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetPipelineRun failed: %v", err)
			}
			if runAfterFailure.Status != "running" {
				t.Fatalf("pipeline run status = %q after branch failure, want running (partial failure must not cascade to failPipelineRun)", runAfterFailure.Status)
			}
			if strings.TrimSpace(runAfterFailure.FailureReason) != "" {
				t.Fatalf("pipeline run failure_reason = %q after branch failure, want empty", runAfterFailure.FailureReason)
			}

			// Simulate a sibling branch succeeding.
			succeedStepRun := &data.PipelineStepRun{
				ID:        "succeeding-branch-" + tc.name,
				RunID:     run.ID,
				StepIndex: 0,
				Status:    "queued",
			}
			if err := systemSvc.CreateStepRun(ctx, succeedStepRun); err != nil {
				t.Fatalf("CreateStepRun(sibling) failed: %v", err)
			}
			tc.setup(t, engine, func(ctx context.Context) (string, error) {
				return `{"status":"succeeded","result":{"ok":true}}`, nil
			})
			ok = tc.executeFn(engine)(ctx, systemSvc, run, PipelineStep{
				Runner:     tc.runnerKind,
				ToAgentID:  target.ID,
				NextMethod: "inspect_market",
				FanOut:     true,
				FanOutKey:  "items",
			}, 0, nil, map[string]any{"condition_id": "b"}, succeedStepRun)
			if !ok {
				t.Fatalf("%s sibling branch returned false on injected success, want true", tc.name)
			}

			// Once both branches are terminal, resolveRunStatus (which
			// executeFanOutStep calls after wg.Wait) must land the pipeline
			// on "completed": at least one final-step branch succeeded, so
			// the run is not all-failed.
			engine.resolveRunStatus(ctx, systemSvc, run.ID)
			finalRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetPipelineRun final failed: %v", err)
			}
			if finalRun.Status != "completed" {
				t.Fatalf("final run status = %q, want completed (one success after one failure)", finalRun.Status)
			}
		})

		t.Run(tc.name+"/runner_error", func(t *testing.T) {
			// Simulates what runClaudeCode / runCodex return after their
			// internal context.WithTimeout(ctx, pipelineRunnerDefaultTimeoutSec)
			// fires (or any other subprocess failure): the provider-specific
			// *ExecutionError type that each runner emits. This injection
			// path deliberately bypasses the actual context.WithTimeout /
			// exec.CommandContext plumbing in runClaudeCode / runCodex —
			// those are covered by their own build-sandbox and
			// semaphore-ordering tests. What matters at the shared-function
			// layer is that the returned *ExecutionError flows through
			// executePipelineStepShared's err != nil branch identically for
			// both runners, including the runner-specific spec.buildFailure
			// type-assertion (each buildFailure expects its own provider's
			// *ExecutionError — a cross-typed injection would silently fall
			// through to the generic err-path and hide divergence, so
			// tc.newExecErr returns the real provider type per runner).
			//
			// External parent-ctx cancellation (engine shutdown) also
			// routes through the same err != nil path in the shared
			// function, but its DB writes fail when the ctx is dead —
			// that case is covered by TestShutdown* in
			// pipelines_core_test.go, which exercises the real runCtx /
			// AfterFunc cancellation chain end-to-end.
			ctx := context.Background()
			db := setupManagerTestDB(t)
			svc := NewAgentService(db)
			engine := NewPipelineEngine(db, svc)
			systemSvc := data.NewAgentService(db, "system")

			target, err := svc.CreateAgent(ctx, "fanout-runner-error-"+tc.name)
			if err != nil {
				t.Fatalf("CreateAgent failed: %v", err)
			}
			pipelineID := "fanout_runner_error_" + tc.name
			if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
				ID:   pipelineID,
				Name: "Fanout Runner Error " + tc.name,
				Steps: []PipelineStep{{
					Runner:     tc.runnerKind,
					OnMethod:   "seed",
					ToAgentID:  target.ID,
					NextMethod: "inspect_market",
					FanOut:     true,
					FanOutKey:  "items",
				}},
			}, true); err != nil {
				t.Fatalf("UpsertPipelineDefinition failed: %v", err)
			}

			now := time.Now().UTC()
			run := &data.PipelineRun{
				ID:           "run-fanout-runner-error-" + tc.name,
				PipelineID:   pipelineID,
				TriggerJobID: "manual",
				CurrentStep:  0,
				Status:       "running",
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
				t.Fatalf("CreatePipelineRun failed: %v", err)
			}

			preCreated := &data.PipelineStepRun{
				ID:        "runner-error-branch-" + tc.name,
				RunID:     run.ID,
				StepIndex: 0,
				Status:    "queued",
			}
			if err := systemSvc.CreateStepRun(ctx, preCreated); err != nil {
				t.Fatalf("CreateStepRun failed: %v", err)
			}

			tc.setup(t, engine, func(ctx context.Context) (string, error) {
				return "", tc.newExecErr(
					fmt.Sprintf("%s timed out after %d seconds", tc.runnerKind, pipelineRunnerDefaultTimeoutSec),
					"context deadline exceeded",
				)
			})

			ok := tc.executeFn(engine)(ctx, systemSvc, run, PipelineStep{
				Runner:     tc.runnerKind,
				ToAgentID:  target.ID,
				NextMethod: "inspect_market",
				FanOut:     true,
				FanOutKey:  "items",
			}, 0, nil, map[string]any{"condition_id": "a"}, preCreated)
			if ok {
				t.Fatalf("%s fan-out branch returned true on injected runner error, want false", tc.name)
			}

			stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("ListStepRunsForRun failed: %v", err)
			}
			if len(stepRuns) != 1 {
				t.Fatalf("step run count = %d, want 1", len(stepRuns))
			}
			if stepRuns[0].Status != "failed" {
				t.Fatalf("errored branch status = %q, want failed", stepRuns[0].Status)
			}

			// Same cascade-suppression invariant: a branch-level runner
			// error must not call failPipelineRun; the pipeline run must
			// stay "running" so siblings can still complete.
			runAfter, err := systemSvc.GetPipelineRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetPipelineRun failed: %v", err)
			}
			if runAfter.Status != "running" {
				t.Fatalf("pipeline run status = %q after branch runner error, want running (branch error must not cascade)", runAfter.Status)
			}

			// Additionally assert the provider-specific buildFailure
			// type-assertion actually fired on the injected
			// *ExecutionError: the step's recorded job result must carry
			// the stderr artifact we put into the error. A cross-typed
			// injection (e.g. claudellm.ExecutionError handed to the
			// codex spec) would fall through buildFailure's type switch
			// and this field would be empty, so this assertion pins
			// that each runner's buildFailure is wired correctly under
			// fan-out.
			job, err := newLocalA2AQueue(db).GetJob(ctx, stepRuns[0].A2AJobID)
			if err != nil {
				t.Fatalf("GetJob failed: %v", err)
			}
			result, _ := job["result"].(map[string]any)
			if stderr, _ := result["stderr"].(string); !strings.Contains(stderr, "context deadline exceeded") {
				t.Fatalf("%s job.result.stderr = %q, want it to contain the injected stderr — buildFailure's provider-specific type assertion may not have matched", tc.name, stderr)
			}
		})
	}
}

func mustAbsPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", path, err)
	}
	return abs
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

func containsArgTriplet(args []string, first, second, third string) bool {
	for i := 0; i+2 < len(args); i++ {
		if args[i] == first && args[i+1] == second && args[i+2] == third {
			return true
		}
	}
	return false
}
