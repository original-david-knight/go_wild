package objectives_planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	agentnode "github.com/original-david-knight/go_wild/agent_node"
)

// ExecutionResult wraps node results with evaluation outcome.
type ExecutionResult struct {
	NodeResults map[string]json.RawMessage
	Sufficient  bool
}

// blockedTools are tool names that objectives agents must never have access to.
// File, shell, code execution, and browser tools are blocked to prevent
// the autonomous system from modifying the filesystem or running arbitrary commands.
var blockedTools = map[string]bool{
	// File operations
	"read_file": true, "write_file": true, "edit_file": true, "list_directory": true,
	"file_read": true, "file_write": true, "file_edit": true, "file_delete": true,
	// Shell / code execution
	"run_shell": true, "shell": true, "run_python": true, "python": true,
	"run_command": true, "execute_command": true, "claude_code": true,
	// Browser automation
	"browser": true, "browser_navigate": true, "browser_click": true,
	"browser_type": true, "browser_screenshot": true,
}

// CompanyToolLoader is a callback that returns additional tools for a specific company.
// The agent_manager wires this to load company-scoped tools (e.g., Shopify with credentials).
type CompanyToolLoader func(ctx context.Context, companyID string) agentnode.ToolRegistry

// ExecutionEngine bridges the objective system to agent_node for
// actual task execution.
type ExecutionEngine struct {
	apiKey            string
	model             string
	maxConc           int
	baseTools         agentnode.ToolRegistry
	activityLog       *ActivityStore
	evaluator         *PostExecutionEvaluator
	companyToolLoader CompanyToolLoader
}

// NewExecutionEngine creates an engine with the given configuration.
// Only safe, read-only web tools are included. File, shell, code execution,
// and browser tools are never available to objectives agents.
func NewExecutionEngine(cfg Config, activityLog *ActivityStore) *ExecutionEngine {
	tools := sanitizeTools(agentnode.DefaultWebTools())
	return &ExecutionEngine{
		apiKey:      cfg.GeminiAPIKey,
		model:       cfg.Model,
		maxConc:     cfg.MaxConcurrency,
		baseTools:   tools,
		activityLog: activityLog,
	}
}

// sanitizeTools removes any blocked tools from a registry.
func sanitizeTools(tools agentnode.ToolRegistry) agentnode.ToolRegistry {
	safe := make(agentnode.ToolRegistry, len(tools))
	for name, tool := range tools {
		if blockedTools[name] {
			log.Printf("[objectives] blocked tool: %s", name)
			continue
		}
		safe[name] = tool
	}
	return safe
}

// Execute runs a NodeGraph for the given objective, respecting the objective's
// tool allowlist. Returns the execution results.
func (e *ExecutionEngine) Execute(ctx context.Context, objective *Objective, graph *agentnode.NodeGraph) (*ExecutionResult, error) {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil, fmt.Errorf("no execution graph provided")
	}

	// Resolve tool registry for this objective
	tools := e.resolveTools(objective)

	// Log start
	if e.activityLog != nil {
		e.activityLog.LogTaskStarted(ctx, objective.ID, fmt.Sprintf("Executing %d nodes for: %s", len(graph.Nodes), objective.Title))
	}

	// Build node def index for enriching activity logs
	nodeDefs := make(map[agentnode.NodeID]agentnode.NodeDef, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeDefs[n.ID] = n
	}

	// Create event channel for logging
	events := make(chan agentnode.GraphEvent, 100)
	go e.consumeEvents(ctx, objective.ID, nodeDefs, events)

	executor, err := agentnode.NewGraphExecutor(ctx, agentnode.ExecutorConfig{
		APIKey:         e.apiKey,
		DefaultModel:   e.model,
		MaxConcurrency: e.maxConc,
		Tools:          tools,
		Events:         events,
	})
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}

	state := agentnode.NewSharedState()
	if err := executor.Execute(ctx, graph, state); err != nil {
		if e.activityLog != nil {
			e.activityLog.LogTaskFailed(ctx, objective.ID, fmt.Sprintf("Execution failed: %v", err), nil)
		}
		return nil, fmt.Errorf("execute graph: %w", err)
	}

	close(events)

	results := state.Snapshot()

	// Log completion
	if e.activityLog != nil {
		e.activityLog.LogTaskCompleted(ctx, objective.ID, fmt.Sprintf("Completed %d nodes for: %s", len(graph.Nodes), objective.Title), map[string]any{
			"node_count":   len(graph.Nodes),
			"result_count": len(results),
		})
	}

	execResult := &ExecutionResult{NodeResults: results, Sufficient: true}

	// Post-execution evaluation (overrides default sufficient=true)
	if e.evaluator != nil {
		evalResult, err := e.evaluator.Evaluate(ctx, objective, results)
		if err != nil {
			log.Printf("[objectives] evaluation failed for %s: %v", objective.ID, err)
		} else {
			execResult.Sufficient = evalResult.Sufficient
			log.Printf("[objectives] evaluation for %s: sufficient=%v, replan=%s",
				objective.ID, evalResult.Sufficient, evalResult.ReplanLevel)
			if e.activityLog != nil {
				e.activityLog.LogEvent(ctx, &ActivityEvent{
					ObjectiveID: objective.ID,
					EventType:   "evaluation_completed",
					Severity:    SeverityInfo,
					Summary:     fmt.Sprintf("Evaluation: sufficient=%v, replan=%s", evalResult.Sufficient, evalResult.ReplanLevel),
					Details: map[string]any{
						"sufficient":      evalResult.Sufficient,
						"replan_level":    evalResult.ReplanLevel,
						"reasoning":       evalResult.Reasoning,
						"extracted_facts": len(evalResult.ExtractedFacts),
					},
				})
			}
		}
	}

	return execResult, nil
}

// resolveTools builds a ToolRegistry by merging base tools with company-scoped tools.
// If the objective has a CompanyID and a CompanyToolLoader is set, company tools are added.
// The objective ToolAllowlist is enforced to keep planning/execution scoped.
func (e *ExecutionEngine) resolveTools(obj *Objective) agentnode.ToolRegistry {
	// Start with base tools
	merged := make(agentnode.ToolRegistry, len(e.baseTools))
	for name, tool := range e.baseTools {
		merged[name] = tool
	}

	// Add company-scoped tools if available
	if obj != nil && obj.CompanyID != "" && e.companyToolLoader != nil {
		companyTools := e.companyToolLoader(context.Background(), obj.CompanyID)
		for name, tool := range companyTools {
			if !blockedTools[name] {
				merged[name] = tool
			}
		}
	}

	if obj == nil || len(obj.ToolAllowlist) == 0 {
		return merged
	}

	allowed := make(map[string]bool, len(obj.ToolAllowlist))
	for _, name := range obj.ToolAllowlist {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			allowed[trimmed] = true
		}
	}
	if len(allowed) == 0 {
		return agentnode.ToolRegistry{}
	}

	filtered := make(agentnode.ToolRegistry, len(allowed))
	for name, tool := range merged {
		if allowed[name] {
			filtered[name] = tool
		}
	}

	var missing []string
	for name := range allowed {
		if _, ok := merged[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		log.Printf("[objectives] objective %s allowlist contains unavailable tools: %v", obj.ID, missing)
	}

	if len(filtered) == 0 {
		log.Printf("[objectives] objective %s allowlist filtered all tools", obj.ID)
	}

	return filtered
}

// SetEvaluator attaches a post-execution evaluator to the engine.
func (e *ExecutionEngine) SetEvaluator(eval *PostExecutionEvaluator) {
	e.evaluator = eval
}

// SetCompanyToolLoader sets a callback to load company-scoped tools (e.g., Shopify).
func (e *ExecutionEngine) SetCompanyToolLoader(loader CompanyToolLoader) {
	e.companyToolLoader = loader
}

// consumeEvents drains the event channel and logs notable events.
// nodeDefs provides the original node definitions so we can log prompt/tools.
func (e *ExecutionEngine) consumeEvents(ctx context.Context, objectiveID string, nodeDefs map[agentnode.NodeID]agentnode.NodeDef, events <-chan agentnode.GraphEvent) {
	for ev := range events {
		switch evt := ev.(type) {
		case agentnode.NodeDoneEvent:
			log.Printf("[objectives] node %s done", evt.NodeID)
			if e.activityLog != nil {
				summary := fmt.Sprintf("Node %s completed", evt.NodeID)
				details := map[string]any{"node_id": string(evt.NodeID)}
				// Include node definition (tools, type) and full prompt with parent context
				if def, ok := nodeDefs[evt.NodeID]; ok {
					if len(def.ToolNames) > 0 {
						details["tools"] = def.ToolNames
					}
					details["node_type"] = string(def.ResolvedType())
				}
				if evt.Result != nil {
					details["prompt"] = evt.Result.FullPrompt
					if evt.Result.Text != "" {
						summary = fmt.Sprintf("Node %s: %s", evt.NodeID, truncateStr(evt.Result.Text, 500))
						details["text"] = evt.Result.Text
					}
					if len(evt.Result.Output) > 0 {
						details["output"] = string(evt.Result.Output)
					}
					details["turns"] = evt.Result.TurnCount
				}
				e.activityLog.LogEvent(ctx, &ActivityEvent{
					ObjectiveID: objectiveID,
					EventType:   "node_completed",
					Severity:    SeverityInfo,
					Summary:     summary,
					Details:     details,
				})
			}
		case agentnode.NodeFailedEvent:
			log.Printf("[objectives] node %s failed: %s", evt.NodeID, evt.Error)
			if e.activityLog != nil {
				details := map[string]any{"node_id": string(evt.NodeID), "error": evt.Error}
				if def, ok := nodeDefs[evt.NodeID]; ok {
					if len(def.ToolNames) > 0 {
						details["tools"] = def.ToolNames
					}
					details["node_type"] = string(def.ResolvedType())
				}
				details["prompt"] = evt.FullPrompt
				e.activityLog.LogTaskFailed(ctx, objectiveID,
					fmt.Sprintf("Node %s failed: %s", evt.NodeID, evt.Error), details)
			}
		case agentnode.NodeSkippedEvent:
			log.Printf("[objectives] node %s skipped: %s", evt.NodeID, evt.Reason)
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
