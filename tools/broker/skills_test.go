package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

func TestSkillsTools_SaveSkill(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.SaveSkillTool(context.Background(), tools.SaveSkillInput{
		Name:        "greet",
		Description: "Greets the user",
		Code:        "def run(name): return f'Hello {name}'",
		InputSchema: map[string]string{"name": "string"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotBody["name"] != "greet" {
		t.Errorf("expected name 'greet', got %v", gotBody["name"])
	}
}

func TestSkillsTools_ListSkills(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"skills": []any{"greet", "summarize"}})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.ListSkillsTool(context.Background(), tools.ListSkillsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSkillsTools_GetSkill(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"name": "greet", "code": "def run(name): return f'Hello {name}'",
		})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.GetSkillTool(context.Background(), tools.GetSkillInput{SkillName: "greet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSkillsTools_DeleteSkill(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.DeleteSkillTool(context.Background(), tools.DeleteSkillInput{SkillName: "greet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSkillsTools_ExecuteSkill_EmptyName(t *testing.T) {
	st := NewSkillsTools(nil, nil)
	result, err := st.ExecuteSkillTool(context.Background(), tools.ExecuteSkillInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty skill name")
	}
}

func TestSkillsTools_ExecuteSkill_NotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty code
		json.NewEncoder(w).Encode(map[string]any{"code": ""})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.ExecuteSkillTool(context.Background(), tools.ExecuteSkillInput{SkillName: "missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for missing skill")
	}
}

func TestSkillsTools_ExecuteSkill_MissingArg(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code":         "def run(name): return name",
			"input_schema": map[string]any{"name": "string"},
		})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.ExecuteSkillTool(context.Background(), tools.ExecuteSkillInput{
		SkillName: "greet",
		Arguments: map[string]any{}, // missing "name"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for missing argument")
	}
}

func TestSkillsTools_ExecuteSkill_BrokerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "db error"})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.ExecuteSkillTool(context.Background(), tools.ExecuteSkillInput{SkillName: "greet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for broker error")
	}
}

func TestSkillsTools_TestSkill_EmptyCode(t *testing.T) {
	st := NewSkillsTools(nil, nil)
	result, err := st.TestSkillTool(context.Background(), tools.TestSkillInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty code")
	}
}

func TestSkillsTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "db error"})
	}))

	st := NewSkillsTools(c, nil)
	result, err := st.SaveSkillTool(context.Background(), tools.SaveSkillInput{Name: "x"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestSkillsTools_DescribeTool(t *testing.T) {
	st := NewSkillsTools(nil, nil)
	if st.DescribeTool("save_skill") == "" {
		t.Error("expected non-empty description")
	}
}

// --- buildSkillScript ---

func TestBuildSkillScript_Simple(t *testing.T) {
	script := buildSkillScript("def run(x): return x*2", nil, map[string]any{"x": 5})
	if !strings.Contains(script, "def run(x): return x*2") {
		t.Error("expected code in script")
	}
	if !strings.Contains(script, "_result = run(**_args)") {
		t.Error("expected run() call in script")
	}
	if !strings.Contains(script, `"x"`) {
		t.Error("expected argument in script")
	}
	// No dependencies = no pip install
	if strings.Contains(script, "pip") {
		t.Error("expected no pip install without dependencies")
	}
}

func TestBuildSkillScript_WithDependencies(t *testing.T) {
	script := buildSkillScript("def run(): pass", []string{"requests", "beautifulsoup4"}, map[string]any{})
	if !strings.Contains(script, "pip") {
		t.Error("expected pip install with dependencies")
	}
	if !strings.Contains(script, "requests") {
		t.Error("expected 'requests' in dependencies")
	}
	if !strings.Contains(script, "beautifulsoup4") {
		t.Error("expected 'beautifulsoup4' in dependencies")
	}
}

func TestBuildSkillScript_NilArguments(t *testing.T) {
	script := buildSkillScript("def run(): return 42", nil, map[string]any{})
	if !strings.Contains(script, "_args = _json.loads") {
		t.Error("expected argument parsing")
	}
	if !strings.Contains(script, "{}") {
		t.Error("expected empty args object")
	}
}

func TestBuildSkillScript_ComplexArguments(t *testing.T) {
	script := buildSkillScript("def run(name, count): pass", nil, map[string]any{
		"name":  "test",
		"count": 42,
	})
	if !strings.Contains(script, `"name"`) {
		t.Error("expected 'name' in args")
	}
	if !strings.Contains(script, "42") {
		t.Error("expected 42 in args")
	}
}
