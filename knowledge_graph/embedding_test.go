package gowild_knowledge_graph

import (
	"math"
	"testing"

	"github.com/original-david-knight/go_wild/agentic_loop"
)

func TestRankBySimilarity(t *testing.T) {
	// Create test nodes with embeddings
	nodes := []Node{
		{
			ID:        "1",
			Name:      "Machine Learning",
			Type:      NodeTypeConcept,
			Embedding: []float32{1.0, 0.0, 0.0}, // Most similar to query
		},
		{
			ID:        "2",
			Name:      "Deep Learning",
			Type:      NodeTypeConcept,
			Embedding: []float32{0.9, 0.1, 0.0}, // Second most similar
		},
		{
			ID:        "3",
			Name:      "Cooking",
			Type:      NodeTypeConcept,
			Embedding: []float32{0.0, 0.0, 1.0}, // Least similar
		},
		{
			ID:        "4",
			Name:      "No Embedding",
			Type:      NodeTypeConcept,
			Embedding: nil, // Should be skipped
		},
		{
			ID:        "5",
			Name:      "Mismatched Dimensions",
			Type:      NodeTypeConcept,
			Embedding: []float32{1.0, 0.0}, // Should be skipped
		},
	}

	queryEmbedding := []float32{1.0, 0.0, 0.0}

	t.Run("rank all with limit", func(t *testing.T) {
		results := rankBySimilarity(queryEmbedding, nodes, 2)
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
		if results[0].Node.ID != "1" {
			t.Errorf("expected first result to be node 1, got %s", results[0].Node.ID)
		}
		if results[1].Node.ID != "2" {
			t.Errorf("expected second result to be node 2, got %s", results[1].Node.ID)
		}
		// First result should have score 1.0
		if math.Abs(float64(results[0].Score-1.0)) > 0.0001 {
			t.Errorf("expected first score to be 1.0, got %f", results[0].Score)
		}
	})

	t.Run("rank all without limit", func(t *testing.T) {
		results := rankBySimilarity(queryEmbedding, nodes, 0)
		// Should return only nodes with embeddings matching query dimensions (3 nodes)
		if len(results) != 3 {
			t.Errorf("expected 3 results, got %d", len(results))
		}
		for _, result := range results {
			if result.Node.ID == "5" {
				t.Error("node with mismatched embedding dimensions should be skipped")
			}
		}
	})

	t.Run("descending order", func(t *testing.T) {
		results := rankBySimilarity(queryEmbedding, nodes, 0)
		for i := 1; i < len(results); i++ {
			if results[i].Score > results[i-1].Score {
				t.Error("results should be in descending order of score")
			}
		}
	})

	t.Run("empty nodes", func(t *testing.T) {
		results := rankBySimilarity(queryEmbedding, []Node{}, 10)
		if len(results) != 0 {
			t.Errorf("expected 0 results for empty nodes, got %d", len(results))
		}
	})
}

func TestFormatNodeForEmbedding(t *testing.T) {
	tests := []struct {
		name     string
		node     *Node
		contains []string
	}{
		{
			name: "basic node",
			node: &Node{
				Name: "Test Node",
				Type: NodeTypeConcept,
			},
			contains: []string{"Test Node", "concept"},
		},
		{
			name: "node with description",
			node: &Node{
				Name: "Test Node",
				Type: NodeTypePerson,
				Properties: map[string]any{
					"description": "A test description",
				},
			},
			contains: []string{"Test Node", "person", "A test description"},
		},
		{
			name: "node with summary",
			node: &Node{
				Name: "Test Node",
				Type: NodeTypeDocument,
				Properties: map[string]any{
					"summary": "This is a summary",
				},
			},
			contains: []string{"Test Node", "document", "This is a summary"},
		},
		{
			name: "node with content",
			node: &Node{
				Name: "Test Node",
				Type: NodeTypeDocument,
				Properties: map[string]any{
					"content": "Some content here",
				},
			},
			contains: []string{"Test Node", "document", "Some content here"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNodeForEmbedding(tt.node)
			for _, s := range tt.contains {
				if !containsString(result, s) {
					t.Errorf("expected result to contain %q, got %q", s, result)
				}
			}
		})
	}
}

func TestFormatNodeForEmbeddingTruncation(t *testing.T) {
	// Create a node with very long content
	longContent := make([]byte, 1000)
	for i := range longContent {
		longContent[i] = 'a'
	}

	node := &Node{
		Name: "Test",
		Type: NodeTypeDocument,
		Properties: map[string]any{
			"content": string(longContent),
		},
	}

	result := formatNodeForEmbedding(node)

	// Result should be truncated to ~500 chars for content + name/type
	if len(result) > 600 {
		t.Errorf("expected result to be truncated, got length %d", len(result))
	}

	// Should end with "..."
	if !containsString(result, "...") {
		t.Error("expected truncated content to end with ...")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || containsString(s[1:], substr)))
}

func TestToolsWithSemanticSearch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tools := NewTools(db, "test-user")

	// Verify ToolProvider interface includes all consolidated tools
	wrapped := gowild_agentic_loop.WrapToolsWithDescriptions(tools)

	// Should have 6 consolidated tools
	if len(wrapped) != 6 {
		t.Errorf("expected 6 tools, got %d", len(wrapped))
	}

	// Verify all tools are present
	toolNames := make(map[string]bool)
	for _, tool := range wrapped {
		toolNames[tool.Name()] = true
	}

	expectedTools := []string{"kg_search", "kg_add", "kg_get", "kg_update", "kg_delete", "kg_explore"}
	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("missing expected tool: %s", expected)
		}
	}

	// Verify all tools have descriptions
	for _, tool := range wrapped {
		if tool.Description() == "" {
			t.Errorf("tool %s has empty description", tool.Name())
		}
	}
}
