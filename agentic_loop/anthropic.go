package gowild_agentic_loop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"google.golang.org/genai"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
	defaultAnthropicTokens  = int32(4096)
)

type anthropicClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string

	modelMu sync.RWMutex
	model   string
}

// newAnthropicClient creates an Anthropic Messages API client.
func newAnthropicClient(_ context.Context, cfg ProviderClientConfig) (*anthropicClient, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &anthropicClient{
		httpClient: defaultHTTPClient(cfg.HTTPClient),
		baseURL:    providerBaseURL(cfg.BaseURL, "ANTHROPIC_BASE_URL", defaultAnthropicBaseURL),
		apiKey:     apiKey,
		model:      model,
	}, nil
}

func (c *anthropicClient) SetModel(model string) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	c.model = model
}

func (c *anthropicClient) GetModel() string {
	c.modelMu.RLock()
	defer c.modelMu.RUnlock()
	return c.model
}

func (c *anthropicClient) Close() error { return nil }

func (c *anthropicClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	model := c.GetModel()
	if config != nil && strings.TrimSpace(config.Model) != "" {
		model = strings.TrimSpace(config.Model)
	}
	if model == "" {
		model = DefaultAnthropicModel
	}
	if config != nil && (config.ResponseSchema != nil || strings.TrimSpace(config.ResponseMIMEType) != "") {
		return nil, fmt.Errorf("anthropic client does not support structured output options")
	}

	messages, err := contentsToAnthropicMessages(contents)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": defaultAnthropicTokens,
	}
	if config != nil {
		if strings.TrimSpace(config.SystemInstruction) != "" {
			body["system"] = config.SystemInstruction
		}
		if config.MaxOutputTokens > 0 {
			body["max_tokens"] = config.MaxOutputTokens
		}
		if config.Temperature != nil {
			body["temperature"] = *config.Temperature
		}
		if config.ThinkingBudget > 0 {
			body["thinking"] = map[string]any{
				"type":          "enabled",
				"budget_tokens": config.ThinkingBudget,
			}
		}
		if tools, err := anthropicToolsFromGemini(config.Tools); err != nil {
			return nil, err
		} else if len(tools) > 0 {
			body["tools"] = tools
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinProviderURL(c.baseURL, "/v1/messages"), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	var bodyResp anthropicMessagesResponse
	if err := decodeProviderResponse(resp, &bodyResp); err != nil {
		return nil, err
	}
	return bodyResp.toGenerateResponse()
}

func anthropicToolsFromGemini(tools []*genai.Tool) ([]map[string]any, error) {
	var out []map[string]any
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}
			schema, err := anthropicSchemaToMap(decl.Parameters)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool schema for %s: %w", decl.Name, err)
			}
			out = append(out, map[string]any{
				"name":         decl.Name,
				"description":  decl.Description,
				"input_schema": schema,
			})
		}
	}
	return out, nil
}

func contentsToAnthropicMessages(contents []*genai.Content) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(contents))
	var pending []providerToolCall
	var pendingToolResults []map[string]any

	flushPendingToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": pendingToolResults,
		})
		pendingToolResults = nil
	}

	for idx, content := range contents {
		if content == nil {
			continue
		}
		if isFunctionResponseContent(content) {
			blocks := make([]map[string]any, 0, len(content.Parts))
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil {
					continue
				}
				callID := consumePendingToolCall(&pending, part.FunctionResponse.Name)
				if callID == "" {
					callID = syntheticToolCallID(idx, part.FunctionResponse.Name)
				}
				payload, err := json.Marshal(part.FunctionResponse.Response)
				if err != nil {
					return nil, fmt.Errorf("failed to encode tool result for %s: %w", part.FunctionResponse.Name, err)
				}
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": callID,
					"content":     string(payload),
				})
			}
			if len(blocks) > 0 {
				pendingToolResults = append(pendingToolResults, blocks...)
			}
			continue
		}
		flushPendingToolResults()

		switch strings.ToLower(strings.TrimSpace(content.Role)) {
		case string(genai.RoleModel):
			blocks, names, ids, err := anthropicAssistantBlocks(content, idx)
			if err != nil {
				return nil, err
			}
			if len(blocks) == 0 {
				continue
			}
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})
			for i := range ids {
				pending = append(pending, providerToolCall{ID: ids[i], Name: names[i]})
			}
		default:
			blocks := anthropicUserBlocks(content)
			if len(blocks) == 0 {
				continue
			}
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": blocks,
			})
		}
	}
	flushPendingToolResults()

	return messages, nil
}

func anthropicMessagesFromContents(contents []*genai.Content) ([]map[string]any, error) {
	return contentsToAnthropicMessages(contents)
}

func anthropicUserBlocks(content *genai.Content) []map[string]any {
	blocks := make([]map[string]any, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		switch {
		case part.Text != "":
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": part.Text,
			})
		case part.InlineData != nil && len(part.InlineData.Data) > 0:
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": part.InlineData.MIMEType,
					"data":       base64.StdEncoding.EncodeToString(part.InlineData.Data),
				},
			})
		}
	}
	return blocks
}

func anthropicAssistantBlocks(content *genai.Content, idx int) ([]map[string]any, []string, []string, error) {
	blocks := make([]map[string]any, 0, len(content.Parts))
	names := make([]string, 0)
	ids := make([]string, 0)
	for _, part := range content.Parts {
		if part == nil || part.Thought {
			continue
		}
		if part.Text != "" {
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": part.Text,
			})
		}
		if part.FunctionCall != nil {
			id := strings.TrimSpace(part.FunctionCall.ID)
			if id == "" {
				id = syntheticToolCallID(idx, part.FunctionCall.Name)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    id,
				"name":  part.FunctionCall.Name,
				"input": part.FunctionCall.Args,
			})
			names = append(names, part.FunctionCall.Name)
			ids = append(ids, id)
		}
	}
	return blocks, names, ids, nil
}

type anthropicMessagesResponse struct {
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Input any    `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (r *anthropicMessagesResponse) toGenerateResponse() (*GenerateResponse, error) {
	if r == nil {
		return &GenerateResponse{}, nil
	}
	content := &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: make([]*genai.Part, 0, len(r.Content)),
	}
	for _, block := range r.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				content.Parts = append(content.Parts, &genai.Part{Text: block.Text})
			}
		case "tool_use":
			args := map[string]any{}
			switch v := block.Input.(type) {
			case map[string]any:
				args = v
			case nil:
			default:
				encoded, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("failed to normalize tool args for %s: %w", block.Name, err)
				}
				if err := json.Unmarshal(encoded, &args); err != nil {
					return nil, fmt.Errorf("failed to decode tool args for %s: %w", block.Name, err)
				}
			}
			content.Parts = append(content.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: args,
				},
			})
		}
	}

	resp := &GenerateResponse{
		Content:      content,
		FinishReason: r.StopReason,
	}
	totalTokens := r.Usage.InputTokens + r.Usage.OutputTokens
	if totalTokens > 0 {
		resp.Usage = &ModelUsage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      totalTokens,
		}
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			resp.FunctionCalls = append(resp.FunctionCalls, part.FunctionCall)
		}
	}
	return resp, nil
}

// GenerateContentStreaming satisfies the streaming seam with the single-delta
// fallback: no native Anthropic streaming path is wired yet (lifedash M17).
func (c *anthropicClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	return SingleDeltaFallback(ctx, c, contents, config, sink)
}
