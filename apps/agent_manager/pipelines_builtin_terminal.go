package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func (pe *PipelineEngine) getBuiltinTerminalHub() *BuiltinTerminalHub {
	if pe == nil {
		return nil
	}
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if pe.builtinTerminal == nil {
		pe.builtinTerminal = newBuiltinTerminalHub()
	}
	return pe.builtinTerminal
}

func (pe *PipelineEngine) publishBuiltinTerminalRequest(run *data.PipelineRun, step PipelineStep, stepIdx int, params map[string]any) {
	hub := pe.getBuiltinTerminalHub()
	if hub == nil {
		return
	}
	hub.PublishText(formatBuiltinTerminalEntry("request", run, step, stepIdx, map[string]any{
		"params": params,
	}))
}

func (pe *PipelineEngine) publishBuiltinTerminalResult(run *data.PipelineRun, step PipelineStep, stepIdx int, duration time.Duration, status string, result map[string]any) {
	hub := pe.getBuiltinTerminalHub()
	if hub == nil {
		return
	}
	payload := map[string]any{
		"duration_ms": duration.Milliseconds(),
		"result":      result,
		"status":      strings.TrimSpace(status),
	}
	hub.PublishText(formatBuiltinTerminalEntry("result", run, step, stepIdx, payload))
}

func (pe *PipelineEngine) publishBuiltinTerminalError(run *data.PipelineRun, step PipelineStep, stepIdx int, duration time.Duration, err error) {
	hub := pe.getBuiltinTerminalHub()
	if hub == nil || err == nil {
		return
	}
	payload := map[string]any{
		"duration_ms": duration.Milliseconds(),
		"error":       err.Error(),
		"status":      "failed",
	}
	hub.PublishText(formatBuiltinTerminalEntry("error", run, step, stepIdx, payload))
}

func formatBuiltinTerminalEntry(kind string, run *data.PipelineRun, step PipelineStep, stepIdx int, payload map[string]any) string {
	meta := map[string]any{
		"event":      strings.TrimSpace(kind),
		"method":     strings.TrimSpace(step.NextMethod),
		"run_id":     "",
		"step_index": stepIdx,
		"time":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if run != nil {
		meta["pipeline_id"] = strings.TrimSpace(run.PipelineID)
		meta["run_id"] = strings.TrimSpace(run.ID)
		if scope := strings.TrimSpace(run.ScopeCompanyID); scope != "" {
			meta["company_id"] = scope
		}
	}
	body := map[string]any{
		"meta": meta,
	}
	if payload != nil {
		body["payload"] = payload
	}

	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return fmt.Sprintf("[%s] builtin %s: %+v\n\n", meta["time"], kind, body)
	}
	return string(encoded) + "\n\n"
}
