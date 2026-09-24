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

// RetrievalEmbedder embeds documents and queries for retrieval with Gemini,
// using the retrieval task types and a reduced output width. Gemini
// normalizes only full-width output, so callers comparing by cosine should
// normalize these vectors.
type RetrievalEmbedder struct {
	client     *genai.Client
	model      string
	dimensions int32
}

// NewRetrievalEmbedder builds a RetrievalEmbedder. apiKey must be set; model
// empty means DefaultEmbeddingModel.
func NewRetrievalEmbedder(ctx context.Context, apiKey, model string, dimensions int) (*RetrievalEmbedder, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("retrieval embedder: no API key")
	}
	if model == "" {
		model = DefaultEmbeddingModel
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	return &RetrievalEmbedder{client: client, model: model, dimensions: int32(dimensions)}, nil
}

func (e *RetrievalEmbedder) embed(ctx context.Context, texts []string, task string) ([][]float32, error) {
	contents := make([]*genai.Content, len(texts))
	for i, t := range texts {
		contents[i] = genai.NewContentFromText(t, genai.RoleUser)
	}
	dims := e.dimensions
	result, err := e.client.Models.EmbedContent(ctx, e.model, contents, &genai.EmbedContentConfig{TaskType: task, OutputDimensionality: &dims})
	if err != nil {
		return nil, fmt.Errorf("failed to embed content: %w", err)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding returned %d vectors for %d texts", len(result.Embeddings), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, emb := range result.Embeddings {
		out[i] = emb.Values
	}
	return out, nil
}

// EmbedDocuments embeds texts to be searched.
func (e *RetrievalEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts, "RETRIEVAL_DOCUMENT")
}

// EmbedQuery embeds a search query.
func (e *RetrievalEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	out, err := e.embed(ctx, []string{text}, "RETRIEVAL_QUERY")
	if err != nil {
		return nil, err
	}
	return out[0], nil
}
