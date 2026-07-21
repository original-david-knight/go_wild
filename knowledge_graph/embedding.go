package gowild_knowledge_graph

import (
	"context"
	"fmt"

	aic "github.com/original-david-knight/go_wild/agentic_loop"
)

// EmbeddingService is an alias for the shared embedding service.
type EmbeddingService = aic.EmbeddingService

// NewEmbeddingService creates a new embedding service.
// If apiKey is empty, it uses the GEMINI_API_KEY environment variable.
func NewEmbeddingService(ctx context.Context, apiKey string) (*EmbeddingService, error) {
	return aic.NewEmbeddingService(ctx, apiKey)
}

// embedNode generates an embedding for a node based on its name, type, and properties.
func embedNode(es *EmbeddingService, ctx context.Context, node *Node) ([]float32, error) {
	text := formatNodeForEmbedding(node)
	return es.Embed(ctx, text)
}

// formatNodeForEmbedding creates a text representation of a node for embedding.
func formatNodeForEmbedding(node *Node) string {
	text := fmt.Sprintf("%s (%s)", node.Name, node.Type)

	if node.Notes != "" {
		notes := node.Notes
		if len(notes) > 500 {
			notes = notes[:500] + "..."
		}
		text += ": " + notes
	}

	if desc, ok := node.Properties["description"].(string); ok && desc != "" {
		text += ". " + desc
	}
	if summary, ok := node.Properties["summary"].(string); ok && summary != "" {
		text += ". " + summary
	}
	if content, ok := node.Properties["content"].(string); ok && content != "" {
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		text += ". " + content
	}

	return text
}

// scoredNode represents a node with its similarity score.
type scoredNode struct {
	Node  Node    `json:"node"`
	Score float32 `json:"score"`
}

// rankBySimilarity ranks nodes by their similarity to a query embedding.
func rankBySimilarity(queryEmbedding []float32, nodes []Node, limit int) []scoredNode {
	scored := make([]scoredNode, 0, len(nodes))

	for _, node := range nodes {
		if len(node.Embedding) == 0 || len(node.Embedding) != len(queryEmbedding) {
			continue
		}
		score := aic.CosineSimilarity(queryEmbedding, node.Embedding)
		scored = append(scored, scoredNode{Node: node, Score: score})
	}

	// Sort by score descending
	for i := 1; i < len(scored); i++ {
		j := i
		for j > 0 && scored[j].Score > scored[j-1].Score {
			scored[j], scored[j-1] = scored[j-1], scored[j]
			j--
		}
	}

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}
