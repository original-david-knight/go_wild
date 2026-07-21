package gowild_agentic_loop

import (
	"context"
	"fmt"
	"math"
	"os"

	"google.golang.org/genai"
)

const (
	// DefaultEmbeddingModel is the default Gemini embedding model.
	DefaultEmbeddingModel = "gemini-embedding-001"
)

// EmbeddingService handles embedding generation using Google Gemini.
type EmbeddingService struct {
	client *genai.Client
	model  string
}

// NewEmbeddingService creates a new embedding service.
// If apiKey is empty, it uses the GEMINI_API_KEY environment variable.
// The default model is "gemini-embedding-001", overridable via KG_EMBEDDING_MODEL.
func NewEmbeddingService(ctx context.Context, apiKey string) (*EmbeddingService, error) {
	model := os.Getenv("KG_EMBEDDING_MODEL")
	if model == "" {
		model = DefaultEmbeddingModel
	}
	return newEmbeddingServiceWithModel(ctx, apiKey, model)
}

// newEmbeddingServiceWithModel creates a new embedding service with a specific model.
func newEmbeddingServiceWithModel(ctx context.Context, apiKey string, model string) (*EmbeddingService, error) {
	var opts *genai.ClientConfig
	if apiKey != "" {
		opts = &genai.ClientConfig{
			APIKey: apiKey,
		}
	}

	client, err := genai.NewClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &EmbeddingService{
		client: client,
		model:  model,
	}, nil
}

// Embed generates an embedding for a single text.
func (s *EmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := s.client.Models.EmbedContent(ctx, s.model, genai.Text(text), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to embed content: %w", err)
	}
	return result.Embeddings[0].Values, nil
}

// BatchEmbed generates embeddings for multiple texts.
func (s *EmbeddingService) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		result, err := s.client.Models.EmbedContent(ctx, s.model, genai.Text(text), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = result.Embeddings[0].Values
	}

	return embeddings, nil
}

// Close closes the embedding service.
func (s *EmbeddingService) Close() error {
	return nil
}

// CosineSimilarity computes the cosine similarity between two embeddings.
// Returns a value between -1 and 1, where 1 means identical.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
