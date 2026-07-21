package gowild_agentic_loop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/genai"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

const codexAuthFileEnv = "OPENAI_CODEX_AUTH_FILE"

type openAIClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
	orgID      string
	projectID  string

	modelMu sync.RWMutex
	model   string
}

type codexAuthFile struct {
	OpenAIAPIKey *string `json:"OPENAI_API_KEY"`
	Tokens       struct {
		AccessToken string `json:"access_token"`
	} `json:"tokens"`
}

// newOpenAIClient creates an OpenAI chat-completions client.
func newOpenAIClient(_ context.Context, cfg ProviderClientConfig) (*openAIClient, error) {
	token, err := resolveOpenAIToken(cfg)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultOpenAIModel
	}
	return &openAIClient{
		httpClient: defaultHTTPClient(cfg.HTTPClient),
		baseURL:    providerBaseURL(cfg.BaseURL, "OPENAI_BASE_URL", defaultOpenAIBaseURL),
		token:      token,
		orgID:      strings.TrimSpace(os.Getenv("OPENAI_ORG_ID")),
		projectID:  strings.TrimSpace(os.Getenv("OPENAI_PROJECT_ID")),
		model:      model,
	}, nil
}

func (c *openAIClient) SetModel(model string) {
	c.modelMu.Lock()
	defer c.modelMu.Unlock()
	c.model = model
}

func (c *openAIClient) GetModel() string {
	c.modelMu.RLock()
	defer c.modelMu.RUnlock()
	return c.model
}

func (c *openAIClient) Close() error { return nil }

func (c *openAIClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	model := c.GetModel()
	if config != nil && strings.TrimSpace(config.Model) != "" {
		model = strings.TrimSpace(config.Model)
	}
	if model == "" {
		model = DefaultOpenAIModel
	}
	if shouldUseOpenAIResponsesAPI(config) {
		return c.generateResponsesContent(ctx, model, contents, config)
	}

	messages, err := openAIMessagesFromContents(contents)
	if err != nil {
		return nil, err
	}
	if config != nil && strings.TrimSpace(config.SystemInstruction) != "" {
		messages = append([]map[string]any{{
			"role":    "system",
			"content": config.SystemInstruction,
		}}, messages...)
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if config != nil {
		if config.Temperature != nil {
			body["temperature"] = *config.Temperature
		}
		if config.MaxOutputTokens > 0 {
			body["max_completion_tokens"] = config.MaxOutputTokens
		}
		if effort := openAIReasoningEffort(config.ThinkingBudget); effort != "" {
			body["reasoning_effort"] = effort
		}
		if tools, err := openAIToolsFromGemini(config.Tools); err != nil {
			return nil, err
		} else if len(tools) > 0 {
			body["tools"] = tools
			body["tool_choice"] = "auto"
		}
		if format, err := openAIResponseFormat(config); err != nil {
			return nil, err
		} else if format != nil {
			body["response_format"] = format
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinProviderURL(c.baseURL, "/v1/chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.orgID != "" {
		req.Header.Set("OpenAI-Organization", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("OpenAI-Project", c.projectID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	var bodyResp openAIChatCompletionResponse
	if err := decodeProviderResponse(resp, &bodyResp); err != nil {
		return nil, err
	}
	return bodyResp.toGenerateResponse()
}

func (c *openAIClient) generateResponsesContent(ctx context.Context, model string, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	body, err := buildOpenAIResponsesRequest(model, contents, config)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode openai responses request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinProviderURL(c.baseURL, "/v1/responses"), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.orgID != "" {
		req.Header.Set("OpenAI-Organization", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("OpenAI-Project", c.projectID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai responses request failed: %w", err)
	}
	defer resp.Body.Close()

	var bodyResp openAIResponsesAPIResponse
	if err := decodeProviderResponse(resp, &bodyResp); err != nil {
		return nil, err
	}
	return bodyResp.toGenerateResponse()
}

func resolveOpenAIToken(cfg ProviderClientConfig) (string, error) {
	switch NormalizeOpenAIAuthMode(cfg.OpenAIAuthMode) {
	case OpenAIAuthModeCodexOAuth:
		return readCodexOpenAIToken()
	default:
		token := strings.TrimSpace(cfg.APIKey)
		if token == "" {
			token = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
		if token == "" {
			return "", fmt.Errorf("OPENAI_API_KEY not set")
		}
		return token, nil
	}
}

func readCodexOpenAIToken() (string, error) {
	if apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); apiKey != "" {
		return apiKey, nil
	}
	auth, path, err := readCodexAuthFile()
	if err != nil {
		return "", err
	}
	if auth.OpenAIAPIKey != nil {
		if apiKey := strings.TrimSpace(*auth.OpenAIAPIKey); apiKey != "" {
			return apiKey, nil
		}
	}
	if token := strings.TrimSpace(auth.Tokens.AccessToken); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("codex auth file %q does not contain OPENAI_API_KEY or access_token", path)
}

func readCodexAccessToken() (string, error) {
	auth, path, err := readCodexAuthFile()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(auth.Tokens.AccessToken)
	if token == "" {
		return "", fmt.Errorf("codex auth file %q does not contain an access_token", path)
	}
	return token, nil
}

func readCodexAuthFile() (*codexAuthFile, string, error) {
	path := strings.TrimSpace(os.Getenv(codexAuthFileEnv))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve home directory for codex auth: %w", err)
		}
		path = filepath.Join(home, ".codex", "auth.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read codex auth file %q: %w", path, err)
	}

	var auth codexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, "", fmt.Errorf("failed to decode codex auth file %q: %w", path, err)
	}
	return &auth, path, nil
}

func buildOpenAIChatRequest(model string, contents []*genai.Content, config *GenerateContentConfig) (map[string]any, error) {
	messages, err := openAIMessagesFromContents(contents)
	if err != nil {
		return nil, err
	}
	if config != nil && strings.TrimSpace(config.SystemInstruction) != "" {
		messages = append([]map[string]any{{
			"role":    "system",
			"content": config.SystemInstruction,
		}}, messages...)
	}

	body := map[string]any{
		"model":    strings.TrimSpace(model),
		"messages": messages,
	}
	if config == nil {
		return body, nil
	}
	if config.Temperature != nil {
		body["temperature"] = *config.Temperature
	}
	if config.MaxOutputTokens > 0 {
		body["max_completion_tokens"] = config.MaxOutputTokens
	}
	if effort := openAIReasoningEffort(config.ThinkingBudget); effort != "" {
		body["reasoning_effort"] = effort
	}
	if tools, err := openAIToolsFromGemini(config.Tools); err != nil {
		return nil, err
	} else if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if format, err := openAIResponseFormat(config); err != nil {
		return nil, err
	} else if format != nil {
		body["response_format"] = format
	}
	return body, nil
}

func buildOpenAIResponsesRequest(model string, contents []*genai.Content, config *GenerateContentConfig) (map[string]any, error) {
	input, err := openAIResponsesInputFromContents(contents)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"model": strings.TrimSpace(model),
		"input": input,
	}
	if config == nil {
		return body, nil
	}
	if strings.TrimSpace(config.SystemInstruction) != "" {
		body["instructions"] = config.SystemInstruction
	}
	if config.Temperature != nil {
		body["temperature"] = *config.Temperature
	}
	if config.MaxOutputTokens > 0 {
		body["max_output_tokens"] = config.MaxOutputTokens
	}
	if effort := openAIReasoningEffort(config.ThinkingBudget); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	if tools, err := openAIResponsesToolsFromGemini(config.Tools); err != nil {
		return nil, err
	} else if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if textConfig, err := openAIResponsesTextConfig(config); err != nil {
		return nil, err
	} else if textConfig != nil {
		body["text"] = textConfig
	}
	return body, nil
}

func shouldUseOpenAIResponsesAPI(config *GenerateContentConfig) bool {
	if config == nil {
		return false
	}
	return len(config.Tools) > 0 && openAIReasoningEffort(config.ThinkingBudget) != ""
}

func openAIReasoningEffort(budget int32) string {
	switch {
	case budget >= 32000:
		return "high"
	case budget >= 8000:
		return "medium"
	case budget > 0:
		return "low"
	default:
		return ""
	}
}

func openAIResponsesToolsFromGemini(tools []*genai.Tool) ([]map[string]any, error) {
	var out []map[string]any
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}
			schema, err := openAISchemaToMap(decl.Parameters)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool schema for %s: %w", decl.Name, err)
			}
			out = append(out, map[string]any{
				"type":        "function",
				"name":        decl.Name,
				"description": decl.Description,
				"parameters":  schema,
			})
		}
	}
	return out, nil
}

func openAIToolsFromGemini(tools []*genai.Tool) ([]map[string]any, error) {
	var out []map[string]any
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}
			schema, err := openAISchemaToMap(decl.Parameters)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool schema for %s: %w", decl.Name, err)
			}
			out = append(out, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        decl.Name,
					"description": decl.Description,
					"parameters":  schema,
				},
			})
		}
	}
	return out, nil
}

func openAIResponsesTextConfig(config *GenerateContentConfig) (map[string]any, error) {
	if config == nil {
		return nil, nil
	}
	if config.ResponseSchema != nil {
		schema, err := openAISchemaToMap(config.ResponseSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert response schema: %w", err)
		}
		return map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "response",
				"schema": schema,
				"strict": true,
			},
		}, nil
	}
	if config.ResponseMIMEType == "application/json" {
		return map[string]any{
			"format": map[string]any{
				"type": "json_object",
			},
		}, nil
	}
	return nil, nil
}

func openAIResponseFormat(config *GenerateContentConfig) (map[string]any, error) {
	if config == nil {
		return nil, nil
	}
	if config.ResponseSchema != nil {
		schema, err := openAISchemaToMap(config.ResponseSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert response schema: %w", err)
		}
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response",
				"schema": schema,
				"strict": true,
			},
		}, nil
	}
	if config.ResponseMIMEType == "application/json" {
		return map[string]any{"type": "json_object"}, nil
	}
	return nil, nil
}

func openAIMessagesFromContents(contents []*genai.Content) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(contents))
	var pending []providerToolCall

	for idx, content := range contents {
		if content == nil {
			continue
		}
		if isFunctionResponseContent(content) {
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil {
					continue
				}
				callID := consumePendingToolCall(&pending, part.FunctionResponse.Name)
				if callID == "" {
					callID = syntheticToolCallID(idx, part.FunctionResponse.Name)
				}
				response := part.FunctionResponse.Response
				if response == nil {
					response = map[string]any{}
				}
				payload, err := json.Marshal(response)
				if err != nil {
					return nil, fmt.Errorf("failed to encode tool result for %s: %w", part.FunctionResponse.Name, err)
				}
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      string(payload),
				})
			}
			continue
		}

		switch strings.ToLower(strings.TrimSpace(content.Role)) {
		case string(genai.RoleModel):
			text, toolCalls, toolIDs, err := openAIAssistantMessage(content, idx)
			if err != nil {
				return nil, err
			}
			if text == "" && len(toolCalls) == 0 {
				continue
			}
			msg := map[string]any{
				"role":    "assistant",
				"content": text,
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
				for i, id := range toolIDs {
					pending = append(pending, providerToolCall{ID: id, Name: toolCalls[i]["function"].(map[string]any)["name"].(string)})
				}
			}
			messages = append(messages, msg)
		default:
			userContent, err := openAIUserContent(content)
			if err != nil {
				return nil, err
			}
			if len(userContent) == 0 {
				continue
			}
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": userContent,
			})
		}
	}

	return messages, nil
}

func openAIResponsesInputFromContents(contents []*genai.Content) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(contents))
	var pending []providerToolCall

	for idx, content := range contents {
		if content == nil {
			continue
		}
		if isFunctionResponseContent(content) {
			for _, part := range content.Parts {
				if part == nil || part.FunctionResponse == nil {
					continue
				}
				response := part.FunctionResponse.Response
				if response == nil {
					response = map[string]any{}
				}
				payload, err := json.Marshal(response)
				if err != nil {
					return nil, fmt.Errorf("failed to encode tool result for %s: %w", part.FunctionResponse.Name, err)
				}
				callID := strings.TrimSpace(part.FunctionResponse.ID)
				if callID == "" {
					callID = consumePendingToolCall(&pending, part.FunctionResponse.Name)
				}
				if callID == "" {
					callID = syntheticToolCallID(idx, part.FunctionResponse.Name)
				}
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  string(payload),
				})
			}
			continue
		}

		contentItems, functionCalls, err := openAIResponsesItemsForContent(content, idx)
		if err != nil {
			return nil, err
		}
		items = append(items, contentItems...)
		items = append(items, functionCalls...)
		for _, call := range functionCalls {
			id, _ := call["call_id"].(string)
			name, _ := call["name"].(string)
			if strings.TrimSpace(id) == "" {
				continue
			}
			pending = append(pending, providerToolCall{ID: id, Name: name})
		}
	}

	return items, nil
}

func openAIResponsesItemsForContent(content *genai.Content, idx int) ([]map[string]any, []map[string]any, error) {
	messageContent := make([]map[string]any, 0, len(content.Parts))
	functionCalls := make([]map[string]any, 0)
	assistantRole := openAIResponsesIsAssistantRole(content.Role)

	for _, part := range content.Parts {
		if part == nil || part.Thought {
			continue
		}
		switch {
		case part.Text != "":
			if assistantRole {
				messageContent = append(messageContent, map[string]any{
					"type": "output_text",
					"text": part.Text,
				})
			} else {
				messageContent = append(messageContent, map[string]any{
					"type": "input_text",
					"text": part.Text,
				})
			}
		case part.InlineData != nil && len(part.InlineData.Data) > 0:
			if assistantRole {
				return nil, nil, fmt.Errorf("openai responses does not support assistant image history items")
			}
			url := "data:" + part.InlineData.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(part.InlineData.Data)
			messageContent = append(messageContent, map[string]any{
				"type":      "input_image",
				"image_url": url,
			})
		case part.FunctionCall != nil:
			args := part.FunctionCall.Args
			if args == nil {
				args = map[string]any{}
			}
			encodedArgs, err := json.Marshal(args)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to encode tool args for %s: %w", part.FunctionCall.Name, err)
			}
			callID := strings.TrimSpace(part.FunctionCall.ID)
			if callID == "" {
				callID = syntheticToolCallID(idx, part.FunctionCall.Name)
			}
			functionCalls = append(functionCalls, map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      part.FunctionCall.Name,
				"arguments": string(encodedArgs),
			})
		}
	}

	var messages []map[string]any
	if len(messageContent) > 0 {
		message := map[string]any{
			"type":    "message",
			"content": messageContent,
		}
		if assistantRole {
			message["id"] = syntheticOpenAIResponseMessageID(idx)
			message["role"] = "assistant"
			message["status"] = "completed"
		} else {
			message["role"] = openAIResponsesInputRole(content.Role)
		}
		messages = append(messages, message)
	}
	return messages, functionCalls, nil
}

func syntheticOpenAIResponseMessageID(index int) string {
	return fmt.Sprintf("msg_%d", index)
}

func openAIResponsesIsAssistantRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant", string(genai.RoleModel):
		return true
	default:
		return false
	}
}

func openAIResponsesInputRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system":
		return "system"
	case "developer":
		return "developer"
	default:
		return "user"
	}
}

func openAIUserContent(content *genai.Content) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		switch {
		case part.Text != "":
			items = append(items, map[string]any{
				"type": "text",
				"text": part.Text,
			})
		case part.InlineData != nil && len(part.InlineData.Data) > 0:
			url := "data:" + part.InlineData.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(part.InlineData.Data)
			items = append(items, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": url,
				},
			})
		}
	}
	return items, nil
}

func openAIAssistantMessage(content *genai.Content, idx int) (string, []map[string]any, []string, error) {
	var text strings.Builder
	toolCalls := make([]map[string]any, 0)
	toolIDs := make([]string, 0)

	for _, part := range content.Parts {
		if part == nil || part.Thought {
			continue
		}
		if part.Text != "" {
			text.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			args := part.FunctionCall.Args
			if args == nil {
				args = map[string]any{}
			}
			encodedArgs, err := json.Marshal(args)
			if err != nil {
				return "", nil, nil, fmt.Errorf("failed to encode tool args for %s: %w", part.FunctionCall.Name, err)
			}
			id := strings.TrimSpace(part.FunctionCall.ID)
			if id == "" {
				id = syntheticToolCallID(idx, part.FunctionCall.Name)
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      part.FunctionCall.Name,
					"arguments": string(encodedArgs),
				},
			})
			toolIDs = append(toolIDs, id)
		}
	}

	return text.String(), toolCalls, toolIDs, nil
}

type openAIChatCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (r *openAIChatCompletionResponse) toGenerateResponse() (*GenerateResponse, error) {
	if r == nil || len(r.Choices) == 0 {
		return &GenerateResponse{}, nil
	}
	choice := r.Choices[0]
	content := &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: make([]*genai.Part, 0, 1+len(choice.Message.ToolCalls)),
	}

	if text := parseOpenAIText(choice.Message.Content); text != "" {
		content.Parts = append(content.Parts, &genai.Part{Text: text})
	}

	for _, tc := range choice.Message.ToolCalls {
		args := map[string]any{}
		if strings.TrimSpace(tc.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("failed to decode tool args for %s: %w", tc.Function.Name, err)
			}
		}
		content.Parts = append(content.Parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	resp := &GenerateResponse{
		Content:      content,
		FinishReason: choice.FinishReason,
	}
	if r.Usage.TotalTokens > 0 {
		resp.Usage = &ModelUsage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		}
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			resp.FunctionCalls = append(resp.FunctionCalls, part.FunctionCall)
		}
	}
	return resp, nil
}

func parseOpenAIText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var out strings.Builder
	for _, block := range blocks {
		if block.Text != "" {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}

type openAIResponsesAPIResponse struct {
	Output []struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Content   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Status string `json:"status"`
}

func (r *openAIResponsesAPIResponse) toGenerateResponse() (*GenerateResponse, error) {
	if r == nil {
		return &GenerateResponse{}, nil
	}

	content := &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: make([]*genai.Part, 0, len(r.Output)),
	}

	for _, item := range r.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					content.Parts = append(content.Parts, &genai.Part{Text: part.Text})
				}
			}
		case "function_call":
			args := map[string]any{}
			if strings.TrimSpace(item.Arguments) != "" {
				if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
					return nil, fmt.Errorf("failed to decode tool args for %s: %w", item.Name, err)
				}
			}
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				callID = item.ID
			}
			content.Parts = append(content.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   callID,
					Name: item.Name,
					Args: args,
				},
			})
		}
	}

	resp := &GenerateResponse{
		Content:      content,
		FinishReason: r.Status,
	}
	if r.Usage.TotalTokens > 0 {
		resp.Usage = &ModelUsage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.TotalTokens,
		}
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			resp.FunctionCalls = append(resp.FunctionCalls, part.FunctionCall)
		}
	}
	return resp, nil
}
