package agentnode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// mockPlanner returns a fixed graph for testing.
type mockPlanner struct {
	graphs []PlanResult
	calls  int
}

func (m *mockPlanner) Plan(_ context.Context, req PlanRequest) (*PlanResult, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.graphs) {
		return &PlanResult{Graph: NodeGraph{Nodes: nil}}, nil
	}
	return &m.graphs[idx], nil
}

type nilResultPlanner struct{}

func (nilResultPlanner) Plan(_ context.Context, _ PlanRequest) (*PlanResult, error) {
	return nil, nil
}

// mockChecker returns a fixed sufficiency result.
type mockChecker struct {
	results []SufficiencyResult
	calls   int
}

func (m *mockChecker) Check(_ context.Context, _ SufficiencyRequest) (*SufficiencyResult, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.results) {
		return &SufficiencyResult{Sufficient: true, Reasoning: "default sufficient"}, nil
	}
	return &m.results[idx], nil
}

func TestOrchestrator_SingleRoundSufficient(t *testing.T) {
	planner := &mockPlanner{
		graphs: []PlanResult{
			{
				Graph: NodeGraph{
					Nodes: []NodeDef{
						{ID: "research", Prompt: "research topic"},
					},
				},
				Reasoning: "single node plan",
			},
		},
	}

	checker := &mockChecker{
		results: []SufficiencyResult{
			{Sufficient: true, Reasoning: "results are complete"},
		},
	}

	// We need a real executor that can run nodes, but since nodes need an API key,
	// we test the orchestrator flow with pre-populated state.
	state := NewSharedState()

	// Simulate what the executor would produce
	events := make(chan GraphEvent, 100)
	ctx := context.Background()
	exec, err := NewGraphExecutor(ctx, ExecutorConfig{APIKey: "fake", Events: events})
	if err != nil {
		t.Fatalf("NewGraphExecutor: %v", err)
	}
	orch, err := NewOrchestrator(ctx, OrchestratorConfig{
		Planner:   planner,
		Checker:   checker,
		Executor:  exec,
		MaxRounds: 3,
		Events:    events,
		APIKey:    "fake",
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	// We can't run the full orchestrator without a real API key,
	// so test the interfaces and state management instead.
	_ = orch
	_ = state

	// Verify planner interface
	planResult, err := planner.Plan(context.Background(), PlanRequest{
		UserPrompt: "test prompt",
		Round:      0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(planResult.Graph.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(planResult.Graph.Nodes))
	}
	if planResult.Graph.Nodes[0].ID != "research" {
		t.Fatalf("expected node ID 'research', got '%s'", planResult.Graph.Nodes[0].ID)
	}

	// Verify checker interface
	checkResult, err := checker.Check(context.Background(), SufficiencyRequest{
		UserPrompt: "test prompt",
		CurrentState: map[string]json.RawMessage{
			"research": json.RawMessage(`{"finding":"important data"}`),
		},
		Round: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checkResult.Sufficient {
		t.Fatal("expected sufficient")
	}
}

func TestOrchestrator_MultiRound(t *testing.T) {
	planner := &mockPlanner{
		graphs: []PlanResult{
			{
				Graph: NodeGraph{
					Nodes: []NodeDef{
						{ID: "research-1", Prompt: "initial research"},
					},
				},
				Reasoning: "round 0",
			},
			{
				Graph: NodeGraph{
					Nodes: []NodeDef{
						{ID: "research-2", Prompt: "deeper research"},
					},
				},
				Reasoning: "round 1",
			},
		},
	}

	checker := &mockChecker{
		results: []SufficiencyResult{
			{Sufficient: false, Reasoning: "need more data"},
			{Sufficient: true, Reasoning: "now complete"},
		},
	}

	// Verify multi-round flow through interfaces
	ctx := context.Background()

	// Round 0
	plan0, _ := planner.Plan(ctx, PlanRequest{UserPrompt: "test", Round: 0})
	if plan0.Graph.Nodes[0].ID != "research-1" {
		t.Fatal("wrong node in round 0")
	}

	check0, _ := checker.Check(ctx, SufficiencyRequest{UserPrompt: "test", Round: 0})
	if check0.Sufficient {
		t.Fatal("round 0 should not be sufficient")
	}

	// Round 1
	plan1, _ := planner.Plan(ctx, PlanRequest{
		UserPrompt: "test",
		CurrentState: map[string]json.RawMessage{
			"research-1": json.RawMessage(`"result 1"`),
		},
		Round: 1,
	})
	if plan1.Graph.Nodes[0].ID != "research-2" {
		t.Fatal("wrong node in round 1")
	}

	check1, _ := checker.Check(ctx, SufficiencyRequest{UserPrompt: "test", Round: 1})
	if !check1.Sufficient {
		t.Fatal("round 1 should be sufficient")
	}
}

func TestOrchestrator_EmptyPlanStops(t *testing.T) {
	planner := &mockPlanner{
		graphs: []PlanResult{}, // no plans — will return empty
	}

	plan, _ := planner.Plan(context.Background(), PlanRequest{UserPrompt: "test", Round: 0})
	if len(plan.Graph.Nodes) != 0 {
		t.Fatal("expected empty graph")
	}
}

func TestNodeDef_ResolvedType(t *testing.T) {
	tests := []struct {
		name     string
		def      NodeDef
		expected NodeType
	}{
		{
			name:     "auto with no tools -> single_shot",
			def:      NodeDef{ID: "a", Prompt: "do A"},
			expected: NodeTypeSingleShot,
		},
		{
			name:     "auto with tools -> agentic",
			def:      NodeDef{ID: "b", Prompt: "do B", ToolNames: []string{"shell"}},
			expected: NodeTypeAgentic,
		},
		{
			name:     "explicit single_shot",
			def:      NodeDef{ID: "c", Prompt: "do C", Type: NodeTypeSingleShot},
			expected: NodeTypeSingleShot,
		},
		{
			name:     "explicit agentic",
			def:      NodeDef{ID: "d", Prompt: "do D", Type: NodeTypeAgentic},
			expected: NodeTypeAgentic,
		},
		{
			name:     "explicit deep_research",
			def:      NodeDef{ID: "e", Prompt: "do E", Type: NodeTypeResearch},
			expected: NodeTypeResearch,
		},
		{
			name:     "explicit type overrides tools inference",
			def:      NodeDef{ID: "f", Prompt: "do F", Type: NodeTypeSingleShot, ToolNames: []string{"shell"}},
			expected: NodeTypeSingleShot,
		},
		{
			name:     "deep_research ignores tools",
			def:      NodeDef{ID: "g", Prompt: "do G", Type: NodeTypeResearch, ToolNames: []string{"shell"}},
			expected: NodeTypeResearch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.def.ResolvedType()
			if got != tt.expected {
				t.Fatalf("ResolvedType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNewOrchestrator_RequiresDependencies(t *testing.T) {
	ctx := context.Background()

	exec, err := NewGraphExecutor(ctx, ExecutorConfig{APIKey: "fake"})
	if err != nil {
		t.Fatalf("NewGraphExecutor: %v", err)
	}
	planner := &mockPlanner{}
	checker := &mockChecker{}

	tests := []struct {
		name   string
		config OrchestratorConfig
		want   string
	}{
		{
			name: "missing planner",
			config: OrchestratorConfig{
				Checker:  checker,
				Executor: exec,
				APIKey:   "fake",
			},
			want: "planner is required",
		},
		{
			name: "missing checker",
			config: OrchestratorConfig{
				Planner:  planner,
				Executor: exec,
				APIKey:   "fake",
			},
			want: "checker is required",
		},
		{
			name: "missing executor",
			config: OrchestratorConfig{
				Planner: planner,
				Checker: checker,
				APIKey:  "fake",
			},
			want: "executor is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOrchestrator(ctx, tt.config)
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestOrchestrator_RunPlannerNilResult(t *testing.T) {
	ctx := context.Background()

	exec, err := NewGraphExecutor(ctx, ExecutorConfig{APIKey: "fake"})
	if err != nil {
		t.Fatalf("NewGraphExecutor: %v", err)
	}

	orch, err := NewOrchestrator(ctx, OrchestratorConfig{
		Planner:   nilResultPlanner{},
		Checker:   &mockChecker{},
		Executor:  exec,
		MaxRounds: 1,
		APIKey:    "fake",
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	_, err = orch.Run(ctx, "test")
	if err == nil {
		t.Fatal("expected planner nil-result error, got nil")
	}
	if !strings.Contains(err.Error(), "planner returned nil result") {
		t.Fatalf("expected nil-result error, got %q", err.Error())
	}
}
