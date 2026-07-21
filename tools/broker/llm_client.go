package broker

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// LLMClient implements loop.LLMClient by proxying requests through the broker API.
type LLMClient struct {
	client *Client
	model  string
}

// NewLLMClient creates a new broker-backed LLM client.
func NewLLMClient(brokerClient *Client, model string) *LLMClient {
	return &LLMClient{
		client: brokerClient,
		model:  model,
	}
}

// brokerLLMRequest matches the manager's BrokerLLMRequest.
type brokerLLMRequest struct {
	Contents          []loop.SerializedContent `json:"contents"`
	SystemInstruction string                   `json:"system_instruction,omitempty"`
	Tools             json.RawMessage          `json:"tools,omitempty"`
	Temperature       *float32                 `json:"temperature,omitempty"`
	MaxOutputTokens   int32                    `json:"max_output_tokens,omitempty"`
	ThinkingBudget    int32                    `json:"thinking_budget,omitempty"`
	Model             string                   `json:"model,omitempty"`
}

// brokerLLMResponse matches the manager's BrokerLLMResponse.
type brokerLLMResponse struct {
	Content      *loop.SerializedContent `json:"content,omitempty"`
	Usage        *loop.ModelUsage        `json:"usage,omitempty"`
	FinishReason string                  `json:"finish_reason,omitempty"`
}

// GenerateContent implements loop.LLMClient.
func (c *LLMClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *loop.GenerateContentConfig) (*loop.GenerateResponse, error) {
	// Serialize contents
	serializedContents := loop.SerializeContents(contents)
	model := c.model
	if config != nil && config.Model != "" {
		model = config.Model
	}

	req := brokerLLMRequest{
		Contents: serializedContents,
		Model:    model,
	}

	if config != nil {
		req.SystemInstruction = config.SystemInstruction
		req.Temperature = config.Temperature
		req.MaxOutputTokens = config.MaxOutputTokens
		req.ThinkingBudget = config.ThinkingBudget

		if config.Tools != nil {
			toolsJSON, err := loop.SerializeTools(config.Tools)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize tools: %w", err)
			}
			req.Tools = toolsJSON
		}
	}

	// Send request
	respData, err := c.client.PostRaw(ctx, "/broker/v1/llm/generate", req)
	if err != nil {
		return nil, err
	}

	// Deserialize response
	var brokerResp brokerLLMResponse
	if err := json.Unmarshal(respData, &brokerResp); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	result := &loop.GenerateResponse{
		FinishReason: brokerResp.FinishReason,
		Usage:        brokerResp.Usage,
	}

	if brokerResp.Content != nil {
		content, err := loop.DeserializeContent(*brokerResp.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize content: %w", err)
		}
		result.Content = content

		// Extract function calls
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				result.FunctionCalls = append(result.FunctionCalls, part.FunctionCall)
			}
		}
	}

	return result, nil
}

// SetModel implements loop.LLMClient.
func (c *LLMClient) SetModel(model string) {
	c.model = model
}

// GetModel implements loop.LLMClient.
func (c *LLMClient) GetModel() string {
	return c.model
}

// Close implements loop.LLMClient.
func (c *LLMClient) Close() error {
	return nil
}
