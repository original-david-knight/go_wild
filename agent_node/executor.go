package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	loop "github.com/original-david-knight/go_wild/agentic_loop"

	"google.golang.org/genai"
)

// ToolRegistry maps tool names to agentic loop tools.
type ToolRegistry map[string]loop.Tool

// ExecutorConfig configures the graph executor.
type ExecutorConfig struct {
	APIKey         string
	DefaultModel   string       // default: "gemini-3-flash-preview"
	MaxConcurrency int          // default: 8
	Tools          ToolRegistry // available tools for agentic nodes
	Events         chan<- GraphEvent
}

// GraphExecutor schedules and runs a DAG of nodes concurrently.
type GraphExecutor struct {
	config ExecutorConfig
	client *genai.Client // shared client for single-shot nodes
}

// NewGraphExecutor creates a new executor with a pooled genai client.
func NewGraphExecutor(ctx context.Context, config ExecutorConfig) (*GraphExecutor, error) {
	if config.DefaultModel == "" {
		config.DefaultModel = "gemini-3-flash-preview"
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 8
	}
	if config.APIKey == "" {
		config.APIKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: config.APIKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	return &GraphExecutor{config: config, client: client}, nil
}

// Execute runs all nodes in the graph, respecting dependencies.
// Results are stored in the provided SharedState.
func (e *GraphExecutor) Execute(ctx context.Context, graph *NodeGraph, state *SharedState) error {
	if err := graph.Validate(); err != nil {
		return fmt.Errorf("invalid graph: %w", err)
	}

	nodeMap := make(map[NodeID]*NodeDef, len(graph.Nodes))
	inDegree := make(map[NodeID]int, len(graph.Nodes))
	dependents := make(map[NodeID][]NodeID, len(graph.Nodes))

	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		nodeMap[n.ID] = n
		inDegree[n.ID] = len(n.DependsOn)
		for _, dep := range n.DependsOn {
			dependents[dep] = append(dependents[dep], n.ID)
		}
	}

	ready := make(chan *NodeDef, len(graph.Nodes))
	done := make(chan *NodeResult, len(graph.Nodes))

	// Seed ready channel with root nodes
	for _, n := range graph.Nodes {
		if inDegree[n.ID] == 0 {
			ready <- nodeMap[n.ID]
		}
	}

	// Worker pool
	var wg sync.WaitGroup
	sem := make(chan struct{}, e.config.MaxConcurrency)

	go func() {
		for def := range ready {
			sem <- struct{}{}
			wg.Add(1)
			go func(d *NodeDef) {
				defer func() {
					<-sem
					wg.Done()
				}()
				result := e.executeNode(ctx, d, state)
				done <- result
			}(def)
		}
	}()

	// Main scheduling loop
	remaining := len(graph.Nodes)
	for remaining > 0 {
		result := <-done
		remaining--

		state.set(result.NodeID, result)

		if e.config.Events != nil {
			switch result.Status {
			case NodeDone:
				e.config.Events <- NodeDoneEvent{NodeID: result.NodeID, Result: result}
			case NodeFailed:
				e.config.Events <- NodeFailedEvent{NodeID: result.NodeID, Error: result.Error, FullPrompt: result.FullPrompt}
			}
		}

		// Update dependents
		if result.Status == NodeFailed {
			// Cascade skip through all transitive dependents
			remaining -= e.cascadeSkip(result.NodeID, dependents, state)
		} else {
			for _, depID := range dependents[result.NodeID] {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					ready <- nodeMap[depID]
				}
			}
		}
	}

	close(ready)
	wg.Wait()

	return nil
}

// cascadeSkip marks all transitive dependents of a failed node as skipped.
// Returns the number of nodes skipped.
func (e *GraphExecutor) cascadeSkip(failedID NodeID, dependents map[NodeID][]NodeID, state *SharedState) int {
	skipped := 0
	queue := []NodeID{failedID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, depID := range dependents[id] {
			if state.get(depID) != nil {
				continue // already processed
			}
			skipped++
			reason := fmt.Sprintf("dependency %s failed", failedID)
			skipResult := &NodeResult{
				NodeID: depID,
				Status: NodeSkipped,
				Error:  reason,
			}
			state.set(depID, skipResult)
			if e.config.Events != nil {
				e.config.Events <- NodeSkippedEvent{NodeID: depID, Reason: reason}
			}
			queue = append(queue, depID)
		}
	}
	return skipped
}

// executeNode runs a single node, either as single-shot or agentic.
func (e *GraphExecutor) executeNode(ctx context.Context, def *NodeDef, state *SharedState) *NodeResult {
	if e.config.Events != nil {
		e.config.Events <- NodeStartEvent{NodeID: def.ID}
	}

	prompt := buildNodePrompt(def, state)
	model := def.Model
	if model == "" {
		model = e.config.DefaultModel
	}

	var result *NodeResult
	switch def.ResolvedType() {
	case NodeTypeResearch:
		result = e.executeDeepResearch(ctx, def, prompt)
	case NodeTypeAgentic:
		result = e.executeAgentic(ctx, def, prompt, model)
	default:
		result = e.executeSingleShot(ctx, def, prompt, model)
	}
	result.FullPrompt = prompt
	return result
}

// executeSingleShot calls the genai SDK directly with structured output.
func (e *GraphExecutor) executeSingleShot(ctx context.Context, def *NodeDef, prompt, model string) *NodeResult {
	genConfig := &genai.GenerateContentConfig{}
	if def.SystemPrompt != "" {
		genConfig.SystemInstruction = genai.NewContentFromText(def.SystemPrompt, genai.RoleUser)
	}
	if def.OutputSchema != nil {
		genConfig.ResponseMIMEType = "application/json"
		genConfig.ResponseSchema = def.OutputSchema
	}

	resp, err := e.client.Models.GenerateContent(ctx, model, genai.Text(prompt), genConfig)
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("generation error: %v", err),
		}
	}

	text := extractCandidateText(resp)
	if strings.TrimSpace(text) == "" {
		return &NodeResult{
			NodeID:    def.ID,
			Status:    NodeFailed,
			Error:     "model returned empty response",
			TurnCount: 1,
		}
	}

	result := &NodeResult{
		NodeID:    def.ID,
		Status:    NodeDone,
		TurnCount: 1,
	}

	// If we had a schema, the response should be JSON
	if def.OutputSchema != nil {
		result.Output = json.RawMessage(text)
	} else {
		result.Text = text
	}

	if resp.UsageMetadata != nil {
		result.Usage = loop.ModelUsage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	return result
}

// executeAgentic runs a multi-turn agentic loop with tools.
func (e *GraphExecutor) executeAgentic(ctx context.Context, def *NodeDef, prompt, model string) *NodeResult {
	// Resolve tools — fail fast if the planner assigned tools that don't exist
	var tools []loop.Tool
	var missing []string
	for _, name := range def.ToolNames {
		if t, ok := e.config.Tools[name]; ok {
			tools = append(tools, t)
		} else {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		availableNames := make([]string, 0, len(e.config.Tools))
		for name := range e.config.Tools {
			availableNames = append(availableNames, name)
		}
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("unknown tools: %v (available: %v)", missing, availableNames),
		}
	}

	maxTurns := def.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}

	opts := []loop.Option{
		loop.WithMaxTurns(maxTurns),
		loop.WithTools(tools...),
	}
	if def.SystemPrompt != "" {
		opts = append(opts, loop.WithSystemPrompt(def.SystemPrompt))
	}

	agLoop, err := loop.New(ctx, e.config.APIKey, model, opts...)
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create agentic loop: %v", err),
		}
	}
	defer agLoop.Close()

	history := []loop.Message{loop.NewUserMessage(prompt)}
	doneEvent, err := agLoop.RunSync(ctx, history)
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("agentic loop error: %v", err),
		}
	}

	return &NodeResult{
		NodeID:    def.ID,
		Status:    NodeDone,
		Text:      doneEvent.FinalText,
		Usage:     doneEvent.Usage,
		TurnCount: doneEvent.TurnCount,
	}
}

// extractCandidateText extracts text from the first candidate's content.
func extractCandidateText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	c := resp.Candidates[0]
	if c == nil || c.Content == nil {
		return ""
	}
	var text string
	for _, part := range c.Content.Parts {
		if part.Thought {
			continue
		}
		if part.Text != "" {
			text += part.Text
		}
	}
	return text
}
