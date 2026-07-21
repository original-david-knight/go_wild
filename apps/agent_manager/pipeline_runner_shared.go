package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	data "github.com/original-david-knight/go_wild/agent_data"
)

// Context keys used by executePipelineStepShared to communicate tool-filter
// decisions to the active runner (Claude Code or Codex). Defined here rather
// than in a provider-specific file because both runners read them and the
// shared runner is the sole writer.
const (
	pipelineDisabledToolsKey   = contextKey("pipeline_disabled_tools")
	pipelineDisableAllToolsKey = contextKey("pipeline_disable_all_tools")
)

// parsedRunnerResult is the provider-neutral shape of a parsed runner output.
// Both claudellm.ParsedResult and codexllm.ParsedResult collapse into this.
type parsedRunnerResult struct {
	Status        string
	Payload       map[string]any
	FailureReason string
	FormatError   bool
}

// pipelineRunnerSpec describes a provider-specific runner for
// executePipelineStepShared. Fields capture the pieces that actually differ
// between Claude Code and Codex; everything else is shared.
type pipelineRunnerSpec struct {
	// label is used in log lines and failure messages (e.g. "claude-code", "codex").
	label string
	// jobIDPrefix is prepended to the uuid for the A2A job ID.
	jobIDPrefix string
	// modelProvider is the value written to agent.ModelProvider for the target
	// agent (e.g. data.LLMProviderAnthropic, data.LLMProviderOpenAI).
	modelProvider string
	// missionIntro is the first line of the mission prompt
	// (e.g. "This is a Claude Code pipeline step.").
	missionIntro string

	// invoke runs the LLM CLI. activate must be called before real work
	// begins; for runners that gate activation on a semaphore, invoke is
	// expected to call activate after acquiring the semaphore.
	invoke func(ctx context.Context, pe *PipelineEngine, agentID, prompt, systemPrompt string, activate func()) (string, error)

	parse          func(string) parsedRunnerResult
	extractFinal   func(string) string
	formatEventLog func(string) string
	buildFailure   func(string, error) (map[string]any, map[string]any)

	// buildCorrectionPrompt produces the retry prompt when the first response
	// fails output-format validation. Shared by default; overridable if a
	// provider needs different wording.
	buildCorrectionPrompt func(failureReason, priorResponse string, methodDef *data.A2AMethod) string

	// deferFanOutActivation, when true, means that for fan-out steps the
	// "running" status transition should be deferred until invoke reports
	// that resources (e.g. the runner semaphore) have been acquired. Codex
	// uses this so queued branches don't appear as stale-running to the
	// orphan detector. Claude activates synchronously in all cases.
	deferFanOutActivation bool
}

// recordPipelineJob upserts the localA2AJob row that tracks a pipeline step's
// runner invocation. Provider-neutral: shared by Claude Code and Codex (and
// any future pipeline runner). The status/result/errPayload/completedAt args
// follow the lifecycle "running" → "succeeded"|"failed", with completedAt set
// only on terminal transitions.
func (pe *PipelineEngine) recordPipelineJob(ctx context.Context, jobID, targetAgentID string, req localA2ARequest, status string, result any, errPayload any, completedAt time.Time) {
	if pe == nil || pe.db == nil || strings.TrimSpace(jobID) == "" {
		return
	}
	now := time.Now().UTC()
	record := &localA2AJob{
		ID:          strings.TrimSpace(jobID),
		FromAgentID: "pipeline",
		ToAgentID:   strings.TrimSpace(targetAgentID),
		RequestJSON: mustJSON(req),
		Status:      strings.TrimSpace(status),
		UpdatedAt:   now,
	}
	if record.Status == "" {
		record.Status = "running"
	}
	if result != nil {
		record.ResultJSON = mustJSON(result)
	}
	if errPayload != nil {
		record.ErrorJSON = mustJSON(errPayload)
	}
	if !completedAt.IsZero() {
		completedUTC := completedAt.UTC()
		record.CompletedAt = &completedUTC
	}

	table := pe.db.Table(localA2AJob{})
	var existing localA2AJob
	if err := table.Get(ctx, record.ID, &existing); err != nil {
		record.CreatedAt = now
		if err := table.Insert(ctx, record); err != nil {
			log.Printf("Pipeline engine: failed to insert pipeline job %s: %v", record.ID, err)
		}
		return
	}

	existing.ToAgentID = record.ToAgentID
	existing.RequestJSON = record.RequestJSON
	existing.Status = record.Status
	existing.UpdatedAt = now
	if result != nil {
		existing.ResultJSON = record.ResultJSON
	}
	if errPayload != nil {
		existing.ErrorJSON = record.ErrorJSON
	}
	if !completedAt.IsZero() {
		existing.CompletedAt = record.CompletedAt
	}
	if err := table.Update(ctx, &existing); err != nil {
		log.Printf("Pipeline engine: failed to update pipeline job %s: %v", existing.ID, err)
	}
}

// loadPipelineMethodDefinition resolves an A2A method name to its database
// record. Used by every pipeline runner (Claude Code, Codex, …) — the method
// definition is provider-neutral and only carries fields like FreshContext,
// RedactMarketPrices, PolymarketNoteAugmentationDisabled, etc. that shape how
// the shared executor builds the mission prompt. Returns nil when the method
// is empty, missing, or the engine has no DB handle.
func (pe *PipelineEngine) loadPipelineMethodDefinition(ctx context.Context, svc *data.AgentService, method string) *data.A2AMethod {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil
	}
	if svc != nil {
		methodDef, err := svc.GetA2AMethod(ctx, method)
		if err == nil {
			return methodDef
		}
	}
	if pe == nil || pe.db == nil {
		return nil
	}
	methodDef, err := data.NewAgentService(pe.db, "system").GetA2AMethod(ctx, method)
	if err != nil {
		return nil
	}
	return methodDef
}

// executePipelineStepShared is the common body of executeClaudeCodeStep and
// executeCodexStep. It handles param merging, step-run lifecycle, target
// agent resolution, mission-prompt assembly, runner invocation, output
// parsing (with one format-correction retry), job recording, and next-step
// triggering. The spec supplies everything provider-specific.
func (pe *PipelineEngine) executePipelineStepShared(
	ctx context.Context,
	svc *data.AgentService,
	run *data.PipelineRun,
	step PipelineStep,
	stepIdx int,
	result map[string]any,
	explicitParams map[string]any,
	spec pipelineRunnerSpec,
	existingStepRun []*data.PipelineStepRun,
) bool {
	params := explicitParams
	if params == nil {
		params = mapParams(result, step.ParamMap)
	}
	if params == nil {
		params = map[string]any{}
	}
	// Always apply literal injections (=key) from the param map, even when
	// explicit params were provided (e.g. from pipeline trigger).
	for k, v := range step.ParamMap {
		if strings.HasPrefix(k, "=") {
			params[strings.TrimPrefix(k, "=")] = v
		}
	}
	params = sanitizePipelineStepParams(ctx, svc, step.NextMethod, params)
	methodDef := pe.loadPipelineMethodDefinition(ctx, svc, step.NextMethod)

	jobID := spec.jobIDPrefix + uuid.New().String()
	now := time.Now()

	fanOut := len(existingStepRun) > 0 && existingStepRun[0] != nil
	var stepRun *data.PipelineStepRun
	stepRunPreCreated := false
	if fanOut {
		stepRun = existingStepRun[0]
		stepRun.A2AJobID = jobID
		if !spec.deferFanOutActivation {
			// Provider activates synchronously — move the step run to
			// "running" now so observers see the transition.
			stepRun.Status = "running"
			stepRun.StartedAt = now
		}
		stepRunPreCreated = true
	} else {
		stepRun = &data.PipelineStepRun{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepIndex: stepIdx,
			A2AJobID:  jobID,
			Status:    "running",
			StartedAt: now,
		}
	}
	request := localA2ARequest{
		Method: step.NextMethod,
		Params: params,
	}
	stepRunPersisted := stepRunPreCreated

	targetAgentID, err := pe.resolvePipelineTargetAgent(ctx, run, step, spec.label)
	pe.ensureAgentModelProvider(ctx, targetAgentID, spec.modelProvider)
	if err != nil {
		pe.recordPipelineJob(ctx, jobID, strings.TrimSpace(step.ToAgentID), request, "failed", nil, map[string]any{"message": err.Error()}, now)
		stepRun.Status = "failed"
		stepRun.CompletedAt = time.Now()
		if stepRunPreCreated {
			if updateErr := svc.UpdateStepRun(ctx, stepRun); updateErr != nil {
				log.Printf("Pipeline engine: failed to update %s step failure: %v", spec.label, updateErr)
			}
		} else if createErr := svc.CreateStepRun(ctx, stepRun); createErr != nil {
			log.Printf("Pipeline engine: failed to record %s step failure: %v", spec.label, createErr)
		}
		pe.failPipelineRun(ctx, svc, run.ID, err.Error())
		return false
	}

	// activateStepRun transitions the step run to "running" and records the
	// job as active. Called synchronously below for non-deferred providers,
	// or deferred into invoke() for providers that gate on a semaphore.
	activateStepRun := func() {
		pe.markClaudeJobActive(jobID)
		stepRun.Status = "running"
		stepRun.StartedAt = time.Now()
		if stepRunPreCreated {
			if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
				log.Printf("Pipeline engine: failed to update %s step run to running: %v", spec.label, err)
			}
		} else if err := svc.CreateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: failed to record %s step run start: %v", spec.label, err)
		} else {
			stepRunPersisted = true
		}
		pe.recordPipelineJob(ctx, jobID, targetAgentID, request, "running", nil, nil, time.Time{})
	}

	deferActivation := spec.deferFanOutActivation && fanOut
	if !deferActivation {
		activateStepRun()
		activateStepRun = nil
	}
	defer pe.clearClaudeJobActive(jobID)

	systemPrompt := pe.loadAgentSystemPrompt(ctx, targetAgentID)
	missionPrompt := buildPipelineMissionPrompt(spec.missionIntro, step.NextMethod, methodDef, params, sanitizePipelinePriorResult(step.NextMethod, methodDef, result))

	ctx = context.WithValue(ctx, brokerExecutionMethodKey, step.NextMethod)
	if methodDef != nil {
		if dt := methodDef.DisabledToolNames(); len(dt) > 0 {
			names := make([]string, 0, len(dt))
			for name := range dt {
				names = append(names, name)
			}
			ctx = context.WithValue(ctx, pipelineDisabledToolsKey, names)
		}
	}
	runnerOutput, err := spec.invoke(ctx, pe, targetAgentID, missionPrompt, systemPrompt, activateStepRun)

	if err != nil {
		failureResult, failurePayload := spec.buildFailure(runnerOutput, err)
		pe.recordPipelineJob(ctx, jobID, targetAgentID, request, "failed", failureResult, failurePayload, time.Now())
		stepRun.Status = "failed"
		stepRun.CompletedAt = time.Now()
		if stepRunPersisted {
			if updateErr := svc.UpdateStepRun(ctx, stepRun); updateErr != nil {
				log.Printf("Pipeline engine: failed to update %s step failure: %v", spec.label, updateErr)
			}
		} else if createErr := svc.CreateStepRun(ctx, stepRun); createErr != nil {
			log.Printf("Pipeline engine: failed to record %s step failure: %v", spec.label, createErr)
		}
		if !fanOut {
			pe.failPipelineRun(ctx, svc, run.ID, fmt.Sprintf("%s step %s failed: %v", spec.label, step.NextMethod, err))
		}
		return false
	}

	stepRun.CompletedAt = time.Now()

	parsedResult := spec.parse(runnerOutput)

	// If the output format was wrong (not a genuine LLM failure), attempt one
	// correction: re-invoke the runner with the malformed response and ask it
	// to reformat as the required JSON envelope.
	if parsedResult.FormatError {
		correctionPrompt := spec.buildCorrectionPrompt(parsedResult.FailureReason, spec.extractFinal(runnerOutput), methodDef)
		log.Printf("Pipeline engine: attempting %s output format correction for step %s (reason: %s)", spec.label, step.NextMethod, parsedResult.FailureReason)
		correctionCtx := context.WithValue(ctx, pipelineDisableAllToolsKey, true)
		correctionOutput, correctionErr := spec.invoke(correctionCtx, pe, targetAgentID, correctionPrompt, "", nil)
		if correctionErr == nil {
			corrected := spec.parse(correctionOutput)
			if !corrected.FormatError {
				log.Printf("Pipeline engine: %s output format correction succeeded for step %s", spec.label, step.NextMethod)
				parsedResult = corrected
				runnerOutput = correctionOutput
			} else {
				log.Printf("Pipeline engine: %s output format correction also malformed for step %s", spec.label, step.NextMethod)
			}
		} else {
			log.Printf("Pipeline engine: %s output format correction failed for step %s: %v", spec.label, step.NextMethod, correctionErr)
		}
	}

	methodResult := parsedResult.Payload
	jobStatus := parsedResult.Status
	if jobStatus == "" {
		jobStatus = "succeeded"
	}

	jobResult := map[string]any{
		"id":     jobID,
		"status": jobStatus,
		"request": map[string]any{
			"method": step.NextMethod,
			"params": params,
		},
		"result": methodResult,
	}
	effectiveStatus, payloadFailureReason := pe.effectiveCompletionStatus(jobResult)
	stepRun.Status = effectiveStatus
	if stepRun.Status == "" {
		stepRun.Status = "failed"
	}

	// Resolve the full failure reason before recording anything, so the error
	// payload and pipeline-run failure message are consistent.
	if effectiveStatus != "succeeded" {
		if payloadFailureReason == "" {
			payloadFailureReason = strings.TrimSpace(parsedResult.FailureReason)
		}
		if payloadFailureReason == "" {
			payloadFailureReason = fmt.Sprintf("%s step %s returned status %s", spec.label, step.NextMethod, effectiveStatus)
		}
	}

	failureReason := strings.TrimSpace(parsedResult.FailureReason)
	if payloadFailureReason != "" {
		failureReason = payloadFailureReason
	}
	providerJobResult := map[string]any{
		"status":         strings.TrimSpace(jobStatus),
		"result":         methodResult,
		"event_log":      spec.formatEventLog(runnerOutput),
		"raw_output":     strings.TrimSpace(runnerOutput),
		"failure_reason": failureReason,
	}
	var errorPayload map[string]any
	if effectiveStatus != "succeeded" {
		errorPayload = map[string]any{"message": payloadFailureReason}
	}
	pe.recordPipelineJob(ctx, jobID, targetAgentID, request, stepRun.Status, providerJobResult, errorPayload, time.Now())
	if stepRunPersisted {
		if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: failed to update %s step run: %v", spec.label, err)
		}
	} else if err := svc.CreateStepRun(ctx, stepRun); err != nil {
		log.Printf("Pipeline engine: failed to record %s step run: %v", spec.label, err)
	}

	setCompletionMarketProperties(ctx, pe.db, step.NextMethod, effectiveStatus, extractCompletionConditionID(jobResult), run.ScopeCompanyID)

	if effectiveStatus != "succeeded" {
		if !fanOut {
			pe.failPipelineRun(ctx, svc, run.ID, payloadFailureReason)
		}
		return false
	}

	pe.triggerNextSteps(ctx, svc, stepRun, jobResult)
	return true
}

// resolvePipelineTargetAgent is the shared target-agent resolver for all
// pipeline runners. label is used to produce a provider-appropriate error
// prefix ("claude-code", "codex").
func (pe *PipelineEngine) resolvePipelineTargetAgent(ctx context.Context, run *data.PipelineRun, step PipelineStep, label string) (string, error) {
	targetAgentID := strings.TrimSpace(step.ToAgentID)
	if targetAgentID == "" {
		return "", fmt.Errorf("%s step requires to_agent_id", label)
	}

	if pe.service != nil {
		if _, err := pe.service.GetAgent(ctx, targetAgentID); err != nil {
			return "", fmt.Errorf("%s target agent %q not found: %w", label, targetAgentID, err)
		}
		if strings.TrimSpace(run.ScopeMode) == "company" {
			member, err := pe.service.GetCompanyMemberForAgent(ctx, targetAgentID)
			if err != nil || member == nil || strings.TrimSpace(member.CompanyID) != strings.TrimSpace(run.ScopeCompanyID) {
				return "", fmt.Errorf(companyScopeNoEligibleAgentsFailureReason)
			}
		}
		return targetAgentID, nil
	}

	if pe.db != nil {
		if _, err := data.NewAgentService(pe.db, targetAgentID).GetAgent(ctx); err != nil {
			return "", fmt.Errorf("%s target agent %q not found: %w", label, targetAgentID, err)
		}
		if strings.TrimSpace(run.ScopeMode) == "company" {
			member, err := data.GetCompanyMemberForAgent(ctx, pe.db, targetAgentID)
			if err != nil || member == nil || strings.TrimSpace(member.CompanyID) != strings.TrimSpace(run.ScopeCompanyID) {
				return "", fmt.Errorf(companyScopeNoEligibleAgentsFailureReason)
			}
		}
	}

	return targetAgentID, nil
}

// buildPipelineMissionPrompt assembles the mission prompt common to all
// pipeline runners. intro is the first line (e.g. "This is a Claude Code
// pipeline step.") and lets callers distinguish the surrounding context for
// the model without otherwise diverging the prompt text.
func buildPipelineMissionPrompt(intro, method string, methodDef *data.A2AMethod, params, priorResult map[string]any) string {
	var sb strings.Builder

	method = strings.TrimSpace(method)
	if methodDef != nil && strings.TrimSpace(methodDef.Method) != "" {
		method = strings.TrimSpace(methodDef.Method)
	}
	if method == "" {
		method = "(unknown)"
	}

	sb.WriteString(strings.TrimRight(intro, "\n"))
	sb.WriteString("\n\n")
	sb.WriteString("Execute this method call now.\n\n")
	fmt.Fprintf(&sb, "Method: %s\n", method)

	description := ""
	if methodDef != nil {
		description = strings.TrimSpace(methodDef.Description)
	}
	if description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", description)
	}
	if methodDef != nil && methodDef.FreshContext {
		sb.WriteString("Fresh Context: true\n")
	}
	if methodDef != nil && methodDef.DisableMarketNotes {
		sb.WriteString("Market Notes Access: disabled\n")
	}
	if methodDef != nil && methodDef.PolymarketNoteAugmentationDisabled() {
		sb.WriteString("Polymarket Note Augmentation: disabled\n")
	}

	instructions := ""
	if methodDef != nil {
		instructions = strings.TrimSpace(methodDef.Instructions)
	}
	if instructions != "" {
		sb.WriteString("\nMethod Instructions:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n")
	}

	sb.WriteString("\nInput Parameters (JSON):\n")
	sb.WriteString(formatA2AHeartbeatJSON(params))
	sb.WriteString("\n")

	if methodDef != nil {
		if inputSchema := decodeMethodSchemaJSON(methodDef.InputSchemaJSON); inputSchema != nil {
			sb.WriteString("\nInput Schema (JSON):\n")
			sb.WriteString(formatA2AHeartbeatJSON(inputSchema))
			sb.WriteString("\n")
		}
		if outputSchema := decodeMethodSchemaJSON(methodDef.OutputSchemaJSON); outputSchema != nil {
			sb.WriteString("\nOutput Schema (JSON):\n")
			sb.WriteString(formatA2AHeartbeatJSON(outputSchema))
			sb.WriteString("\n")
		}
	}

	if len(priorResult) > 0 {
		priorJSON, err := json.MarshalIndent(priorResult, "", "  ")
		if err == nil && len(priorJSON) < 4096 {
			sb.WriteString("\nContext from Prior Step (JSON):\n")
			sb.Write(priorJSON)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nCompletion Rules:\n")
	sb.WriteString("- Return exactly one JSON object as your final response.\n")
	sb.WriteString("- The final JSON must have a top-level \"status\" string with value \"succeeded\" or \"failed\".\n")
	sb.WriteString("- When status is \"succeeded\", include a top-level \"result\" object. Match the output schema when one is provided.\n")
	sb.WriteString("- When status is \"failed\", include a concise \"reason\" string explaining the failure.\n")
	sb.WriteString("- Use the available MCP tools when needed to complete the method.\n")
	sb.WriteString("- Do not include markdown fences or commentary before or after the final JSON object.\n")

	return sb.String()
}

// buildPipelineCorrectionPrompt builds the retry prompt used after a first
// runner response fails output-format validation. The body is identical for
// every runner; maxChars trims oversized prior responses before injection.
func buildPipelineCorrectionPrompt(failureReason, priorResponse string, methodDef *data.A2AMethod, maxChars int) string {
	if maxChars > 0 && len(priorResponse) > maxChars {
		priorResponse = priorResponse[:maxChars] + "\n... (truncated)"
	}

	var sb strings.Builder
	sb.WriteString("Your previous response did not meet the required output format.\n\n")
	if failureReason != "" {
		fmt.Fprintf(&sb, "Error: %s\n\n", failureReason)
	}
	sb.WriteString("Your response was:\n")
	sb.WriteString(priorResponse)
	sb.WriteString("\n\n")

	sb.WriteString("Reformat this into the required JSON envelope. The rules are:\n")
	sb.WriteString("- Return exactly one JSON object.\n")
	sb.WriteString("- The JSON must have a top-level \"status\" string with value \"succeeded\" or \"failed\".\n")
	sb.WriteString("- When status is \"succeeded\", include a top-level \"result\" object.\n")
	sb.WriteString("- When status is \"failed\", include a concise \"reason\" string.\n")
	sb.WriteString("- Do not perform new research, do not call tools, and do not continue the task.\n")
	sb.WriteString("- Only transform the provided response into the required JSON envelope.\n")
	sb.WriteString("- Do not include markdown fences or commentary before or after the JSON object.\n")

	if methodDef != nil {
		if outputSchema := decodeMethodSchemaJSON(methodDef.OutputSchemaJSON); outputSchema != nil {
			sb.WriteString("\nThe \"result\" object must match this schema:\n")
			sb.WriteString(formatA2AHeartbeatJSON(outputSchema))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
