package agentnode

import (
	"strings"
	"testing"

	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

func TestBuildResearchResult_WithStructuredOutput(t *testing.T) {
	dr := deepresearch.Result{
		Summary: "complete summary",
		Rounds:  3,
		Output: map[string]any{
			"answer": "ok",
		},
	}

	got := buildResearchResult("node-1", dr, false)
	if got.NodeID != "node-1" {
		t.Fatalf("NodeID = %q, want node-1", got.NodeID)
	}
	if got.Status != NodeDone {
		t.Fatalf("Status = %q, want %q", got.Status, NodeDone)
	}
	if got.TurnCount != 3 {
		t.Fatalf("TurnCount = %d, want 3", got.TurnCount)
	}
	if got.Text != "complete summary" {
		t.Fatalf("Text = %q, want complete summary", got.Text)
	}
	if len(got.Output) == 0 {
		t.Fatal("expected structured output to be populated")
	}
	if !strings.Contains(string(got.Output), `"answer":"ok"`) {
		t.Fatalf("unexpected output JSON: %s", string(got.Output))
	}
}

func TestBuildResearchResult_PartialPrefix(t *testing.T) {
	dr := deepresearch.Result{
		Summary: "timed out",
		Rounds:  1,
	}

	got := buildResearchResult("node-2", dr, true)
	if got.Text != "[partial] timed out" {
		t.Fatalf("Text = %q, want partial prefix", got.Text)
	}
}
