package objectives_planner

import "testing"

func TestExecutionEngine_ResolveTools_EnforcesAllowlist(t *testing.T) {
	engine := NewExecutionEngine(Config{}, nil)
	all := engine.resolveTools(&Objective{})
	if len(all) == 0 {
		t.Fatal("expected at least one base tool")
	}

	var allowed string
	for name := range all {
		allowed = name
		break
	}

	filtered := engine.resolveTools(&Objective{
		ToolAllowlist: []string{allowed, "missing_tool"},
	})

	if len(filtered) != 1 {
		t.Fatalf("expected exactly one allowed tool, got %d", len(filtered))
	}
	if _, ok := filtered[allowed]; !ok {
		t.Fatalf("expected allowed tool %q to be present", allowed)
	}
}
