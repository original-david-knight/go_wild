package main

import (
	"strings"
	"testing"
)

func TestRenderMermaid_Flowchart(t *testing.T) {
	content := `flowchart TD
    A[Start] --> B[Process]
    B --> C{Decision}
    C -->|Yes| D[End]
    C -->|No| B`

	result := renderMermaid(content)

	if !strings.Contains(result, "Flowchart") {
		t.Error("expected Flowchart header")
	}
	if !strings.Contains(result, "Start") {
		t.Error("expected 'Start' node")
	}
	if !strings.Contains(result, "Process") {
		t.Error("expected 'Process' node")
	}
}

func TestRenderMermaid_Graph(t *testing.T) {
	content := `graph LR
    A[Input] --> B[Output]`

	result := renderMermaid(content)
	if !strings.Contains(result, "Flowchart") {
		t.Error("expected Flowchart header for graph type")
	}
}

func TestRenderMermaid_SequenceDiagram(t *testing.T) {
	content := `sequenceDiagram
    participant A
    participant B
    A->>B: Hello
    B-->>A: Response`

	result := renderMermaid(content)
	if !strings.Contains(result, "Sequence Diagram") {
		t.Error("expected Sequence Diagram header")
	}
}

func TestRenderMermaid_StateDiagram(t *testing.T) {
	content := `stateDiagram-v2
    [*] --> Active
    Active --> Inactive
    Inactive --> [*]`

	result := renderMermaid(content)
	if !strings.Contains(result, "State Diagram") {
		t.Error("expected State Diagram header")
	}
}

func TestRenderMermaid_UnknownType(t *testing.T) {
	content := `pie
    "Dogs" : 386
    "Cats" : 85`

	result := renderMermaid(content)
	// Unknown types use renderFormattedSource
	if !strings.Contains(result, "pie") {
		t.Error("expected diagram type in output")
	}
}

func TestRenderMermaid_EmptyContent(t *testing.T) {
	result := renderMermaid("")
	// Empty content falls through to renderFormattedSource (default case)
	// which still renders a formatted box
	if result == "" {
		t.Error("expected non-empty result even for empty content (formatted source fallback)")
	}
}

func TestRenderMermaidBlocks_WithMermaid(t *testing.T) {
	markdown := "Some text before\n\n```mermaid\nflowchart TD\n    A[Start] --> B[End]\n```\n\nSome text after"

	result := renderMermaidBlocks(markdown)
	if !strings.Contains(result, "Some text before") {
		t.Error("expected text before to be preserved")
	}
	if !strings.Contains(result, "Some text after") {
		t.Error("expected text after to be preserved")
	}
	if !strings.Contains(result, "Flowchart") {
		t.Error("expected mermaid block to be rendered")
	}
	// Original mermaid fence should be replaced
	if strings.Contains(result, "```mermaid") {
		t.Error("mermaid fence should be replaced by rendering")
	}
}

func TestRenderMermaidBlocks_NoMermaid(t *testing.T) {
	markdown := "Regular markdown\n\n```go\nfmt.Println(\"hello\")\n```"

	result := renderMermaidBlocks(markdown)
	if result != markdown {
		t.Error("non-mermaid content should be unchanged")
	}
}

func TestRenderFlowchart_WithLabels(t *testing.T) {
	lines := []string{
		"flowchart TD",
		"    A[Start] -->|begin| B[Process]",
	}
	result := renderFlowchart(lines)
	if !strings.Contains(result, "Flowchart") {
		t.Error("expected Flowchart header")
	}
}

func TestRenderFlowchart_Comments(t *testing.T) {
	lines := []string{
		"flowchart TD",
		"    %% This is a comment",
		"    A[Node]",
	}
	result := renderFlowchart(lines)
	if strings.Contains(result, "%%") {
		t.Error("comments should be skipped")
	}
}

func TestRenderSequenceDiagram_WithNotes(t *testing.T) {
	lines := []string{
		"sequenceDiagram",
		"    participant A",
		"    participant B",
		"    A->>B: Request",
		"    Note over A: Processing",
	}
	result := renderSequenceDiagram(lines)
	if !strings.Contains(result, "Sequence Diagram") {
		t.Error("expected Sequence Diagram header")
	}
}

func TestRenderSequenceDiagram_AutoParticipants(t *testing.T) {
	lines := []string{
		"sequenceDiagram",
		"    Alice->>Bob: Hello",
		"    Bob-->>Alice: Hi",
	}
	result := renderSequenceDiagram(lines)
	if !strings.Contains(result, "Alice") {
		t.Error("expected auto-added participant Alice")
	}
	if !strings.Contains(result, "Bob") {
		t.Error("expected auto-added participant Bob")
	}
}

func TestRenderFormattedSource(t *testing.T) {
	lines := []string{
		"customDiagram",
		"    item1",
		"    item2",
	}
	result := renderFormattedSource(lines)
	if !strings.Contains(result, "customDiagram") {
		t.Error("expected diagram type in header")
	}
	if !strings.Contains(result, "item1") {
		t.Error("expected item1")
	}
	if !strings.Contains(result, "item2") {
		t.Error("expected item2")
	}
}

func TestRenderFormattedSource_EmptyLines(t *testing.T) {
	lines := []string{
		"diagram",
		"",
		"    content",
		"",
	}
	result := renderFormattedSource(lines)
	// Empty lines should be skipped
	if !strings.Contains(result, "content") {
		t.Error("expected content")
	}
}
