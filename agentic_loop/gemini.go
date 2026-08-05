package gowild_agentic_loop

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"sync"

	"google.golang.org/genai"
)

// GeminiClient wraps the Google GenAI client for Gemini API access.
type GeminiClient struct {
	client  *genai.Client
	modelMu sync.RWMutex
	model   string
}

// NewGeminiClient creates a new Gemini client.
// If apiKey is empty, it will use the GEMINI_API_KEY environment variable.
func NewGeminiClient(ctx context.Context, apiKey string, model string) (*GeminiClient, error) {
	return NewGeminiClientWithHTTPClient(ctx, apiKey, model, nil)
}

// NewGeminiClientWithHTTPClient creates a Gemini client over a caller-supplied
// http.Client — the seam for a recording or otherwise instrumented transport,
// matching what the OpenAI and Anthropic paths already honour. A nil client
// keeps the SDK's default.
func NewGeminiClientWithHTTPClient(ctx context.Context, apiKey string, model string, httpClient *http.Client) (*GeminiClient, error) {
	var opts *genai.ClientConfig
	if apiKey != "" || httpClient != nil {
		opts = &genai.ClientConfig{}
		if apiKey != "" {
			opts.APIKey = apiKey
		}
		if httpClient != nil {
			opts.HTTPClient = httpClient
		}
	}

	client, err := genai.NewClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &GeminiClient{
		client: client,
		model:  model,
	}, nil
}

// GenerateContentConfig holds configuration for content generation.
type GenerateContentConfig struct {
	SystemInstruction string
	Tools             []*genai.Tool
	Temperature       *float32
	MaxOutputTokens   int32
	ThinkingBudget    int32         // Token budget for thinking (0 = disabled)
	ResponseMIMEType  string        // e.g. "application/json" for structured output
	ResponseSchema    *genai.Schema // JSON schema for structured output
	Model             string        // Optional per-request model override
}

// SetModel changes the model used by the client.
func (c *GeminiClient) SetModel(model string) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	c.model = model
}

// GetModel returns the current model.
func (c *GeminiClient) GetModel() string {
	c.modelMu.RLock()
	defer c.modelMu.RUnlock()
	return c.model
}

// GenerateResponse holds the response from content generation.
type GenerateResponse struct {
	Content           *genai.Content
	FunctionCalls     []*genai.FunctionCall
	GroundingMetadata *genai.GroundingMetadata
	Usage             *ModelUsage
	FinishReason      string
}

// GenerateContent generates content using the Gemini model.
func (c *GeminiClient) GenerateContent(
	ctx context.Context,
	contents []*genai.Content,
	config *GenerateContentConfig,
) (*GenerateResponse, error) {
	model := c.resolveModel(config)
	genConfig := &genai.GenerateContentConfig{}

	if config != nil {
		if config.SystemInstruction != "" {
			genConfig.SystemInstruction = genai.NewContentFromText(config.SystemInstruction, genai.RoleUser)
		}
		if config.Tools != nil {
			genConfig.Tools = config.Tools
		}
		if config.Temperature != nil {
			genConfig.Temperature = config.Temperature
		}
		if config.MaxOutputTokens > 0 {
			genConfig.MaxOutputTokens = config.MaxOutputTokens
		}
		if config.ThinkingBudget > 0 {
			budget := config.ThinkingBudget
			genConfig.ThinkingConfig = &genai.ThinkingConfig{
				ThinkingBudget: &budget,
			}
		}
		if config.ResponseMIMEType != "" {
			genConfig.ResponseMIMEType = config.ResponseMIMEType
		}
		if config.ResponseSchema != nil {
			genConfig.ResponseSchema = config.ResponseSchema
		}
	}

	resp, err := c.client.Models.GenerateContent(ctx, model, contents, genConfig)
	if err != nil {
		return nil, err
	}

	return parseGenerateResponse(resp), nil
}

// GenerateContentStream generates content with streaming.
func (c *GeminiClient) GenerateContentStream(
	ctx context.Context,
	contents []*genai.Content,
	config *GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	model := c.resolveModel(config)
	genConfig := &genai.GenerateContentConfig{}

	if config != nil {
		if config.SystemInstruction != "" {
			genConfig.SystemInstruction = genai.NewContentFromText(config.SystemInstruction, genai.RoleUser)
		}
		if config.Tools != nil {
			genConfig.Tools = config.Tools
		}
		if config.Temperature != nil {
			genConfig.Temperature = config.Temperature
		}
		if config.MaxOutputTokens > 0 {
			genConfig.MaxOutputTokens = config.MaxOutputTokens
		}
		if config.ThinkingBudget > 0 {
			budget := config.ThinkingBudget
			genConfig.ThinkingConfig = &genai.ThinkingConfig{
				ThinkingBudget: &budget,
			}
		}
		if config.ResponseMIMEType != "" {
			genConfig.ResponseMIMEType = config.ResponseMIMEType
		}
		if config.ResponseSchema != nil {
			genConfig.ResponseSchema = config.ResponseSchema
		}
	}

	return c.client.Models.GenerateContentStream(ctx, model, contents, genConfig)
}

func (c *GeminiClient) resolveModel(config *GenerateContentConfig) string {
	if config != nil && config.Model != "" {
		return config.Model
	}
	return c.GetModel()
}

func parseGenerateResponse(resp *genai.GenerateContentResponse) *GenerateResponse {
	result := &GenerateResponse{}

	if resp == nil {
		return result
	}

	// Extract content from first candidate
	if len(resp.Candidates) > 0 {
		candidate := resp.Candidates[0]
		if candidate == nil {
			return result
		}
		result.Content = candidate.Content
		result.GroundingMetadata = candidate.GroundingMetadata
		result.FinishReason = string(candidate.FinishReason)

		// Extract function calls
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if fc := part.FunctionCall; fc != nil {
					result.FunctionCalls = append(result.FunctionCalls, fc)
				}
			}
		}
	}

	// Extract usage
	if resp.UsageMetadata != nil {
		result.Usage = &ModelUsage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	return result
}

// ExtractText extracts text from a Content object.
// Filters out thought text (model internal reasoning).
func ExtractText(content *genai.Content) string {
	if content == nil {
		return ""
	}

	var text string
	for _, part := range content.Parts {
		// Skip thought parts (internal model reasoning)
		if part.Thought {
			continue
		}
		if part.Text != "" {
			text += part.Text
		}
	}
	return text
}

// Close closes the Gemini client.
func (c *GeminiClient) Close() error {
	// The genai client doesn't have a Close method, but we keep this
	// for future compatibility and interface consistency.
	return nil
}

// GenerateContentStreaming streams one generation over the real Gemini
// streaming API (lifedash M17): each text chunk reaches sink as it arrives,
// and the assembled response is what GenerateContent would have returned.
func (c *GeminiClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	return AssembleStream(c.GenerateContentStream(ctx, contents, config), sink)
}
