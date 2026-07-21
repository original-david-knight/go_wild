package gowild_agentic_loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/genai"
)

func TestBuildOpenAIChatRequest_ReconstructsToolMessages(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("Find the weather", genai.RoleUser),
		{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "call_weather",
						Name: "get_weather",
						Args: map[string]any{"location": "SF"},
					},
				},
			},
		},
		genai.NewContentFromFunctionResponse("get_weather", map[string]any{"temp_f": 68}, genai.RoleUser),
	}

	body, err := buildOpenAIChatRequest("gpt-5.4", contents, &GenerateContentConfig{
		SystemInstruction: "Be concise",
	})
	if err != nil {
		t.Fatalf("buildOpenAIChatRequest returned error: %v", err)
	}

	messages, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("expected messages slice, got %T", body["messages"])
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "system" {
		t.Fatalf("expected system message first, got %#v", messages[0])
	}
	if messages[2]["role"] != "assistant" {
		t.Fatalf("expected assistant tool call message, got %#v", messages[2])
	}
	if messages[3]["role"] != "tool" {
		t.Fatalf("expected tool message, got %#v", messages[3])
	}
	if messages[3]["tool_call_id"] != "call_weather" {
		t.Fatalf("expected tool_call_id call_weather, got %v", messages[3]["tool_call_id"])
	}
	if messages[3]["content"] != `{"temp_f":68}` {
		t.Fatalf("unexpected tool content: %v", messages[3]["content"])
	}
}

func TestBuildOpenAIResponsesRequest_ReconstructsToolItems(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("Find the weather", genai.RoleUser),
		{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "call_weather",
						Name: "get_weather",
						Args: map[string]any{"location": "SF"},
					},
				},
			},
		},
		{
			Role: string(genai.RoleUser),
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						ID:       "call_weather",
						Name:     "get_weather",
						Response: map[string]any{"temp_f": 68},
					},
				},
			},
		},
	}

	body, err := buildOpenAIResponsesRequest("gpt-5.4", contents, &GenerateContentConfig{
		SystemInstruction: "Be concise",
		ThinkingBudget:    24000,
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:       "get_weather",
				Parameters: &genai.Schema{Type: genai.TypeObject},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("buildOpenAIResponsesRequest returned error: %v", err)
	}

	if body["instructions"] != "Be concise" {
		t.Fatalf("expected instructions, got %#v", body["instructions"])
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("expected reasoning effort medium, got %#v", body["reasoning"])
	}
	input, ok := body["input"].([]map[string]any)
	if !ok {
		t.Fatalf("expected input item slice, got %T", body["input"])
	}
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}
	if input[1]["type"] != "function_call" {
		t.Fatalf("expected function_call item, got %#v", input[1])
	}
	if input[2]["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output item, got %#v", input[2])
	}
	if input[2]["call_id"] != "call_weather" {
		t.Fatalf("expected call_weather call_id, got %#v", input[2]["call_id"])
	}
}

func TestBuildOpenAIResponsesRequest_MatchesPendingToolCallForMissingResponseID(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("Find the weather", genai.RoleUser),
		{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "call_weather",
						Name: "get_weather",
						Args: map[string]any{"location": "SF"},
					},
				},
			},
		},
		genai.NewContentFromFunctionResponse("get_weather", map[string]any{"temp_f": 68}, genai.RoleUser),
	}

	body, err := buildOpenAIResponsesRequest("gpt-5.4", contents, nil)
	if err != nil {
		t.Fatalf("buildOpenAIResponsesRequest returned error: %v", err)
	}

	input, ok := body["input"].([]map[string]any)
	if !ok {
		t.Fatalf("expected input item slice, got %T", body["input"])
	}
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}
	if input[2]["call_id"] != "call_weather" {
		t.Fatalf("expected pending tool call ID to be reused, got %#v", input[2]["call_id"])
	}
}

func TestBuildOpenAIResponsesRequest_UsesOutputTextForAssistantHistory(t *testing.T) {
	contents := []*genai.Content{
		genai.NewContentFromText("Question", genai.RoleUser),
		{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{Text: "Previous answer"},
			},
		},
	}

	body, err := buildOpenAIResponsesRequest("gpt-5.4", contents, nil)
	if err != nil {
		t.Fatalf("buildOpenAIResponsesRequest returned error: %v", err)
	}

	input, ok := body["input"].([]map[string]any)
	if !ok {
		t.Fatalf("expected input item slice, got %T", body["input"])
	}
	if len(input) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(input))
	}
	userContent, ok := input[0]["content"].([]map[string]any)
	if !ok || len(userContent) != 1 {
		t.Fatalf("expected one user content block, got %#v", input[0]["content"])
	}
	if userContent[0]["type"] != "input_text" {
		t.Fatalf("expected user input_text block, got %#v", userContent[0])
	}
	if input[1]["role"] != "assistant" {
		t.Fatalf("expected assistant role, got %#v", input[1]["role"])
	}
	if input[1]["status"] != "completed" {
		t.Fatalf("expected completed assistant message, got %#v", input[1]["status"])
	}
	assistantContent, ok := input[1]["content"].([]map[string]any)
	if !ok || len(assistantContent) != 1 {
		t.Fatalf("expected one assistant content block, got %#v", input[1]["content"])
	}
	if assistantContent[0]["type"] != "output_text" {
		t.Fatalf("expected assistant output_text block, got %#v", assistantContent[0])
	}
}

func TestOpenAIResponsesAPIResponse_ToGenerateResponse(t *testing.T) {
	resp, err := (&openAIResponsesAPIResponse{
		Status: "completed",
		Output: []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}{
			{
				Type: "message",
				Content: []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}{
					{Type: "output_text", Text: "hello"},
				},
			},
			{
				Type:      "function_call",
				CallID:    "call_weather",
				Name:      "get_weather",
				Arguments: `{"location":"SF"}`,
			},
		},
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		}{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}).toGenerateResponse()
	if err != nil {
		t.Fatalf("toGenerateResponse returned error: %v", err)
	}
	if resp.FinishReason != "completed" {
		t.Fatalf("expected completed status, got %q", resp.FinishReason)
	}
	if resp.Content == nil || len(resp.Content.Parts) != 2 {
		t.Fatalf("expected 2 content parts, got %#v", resp.Content)
	}
	if resp.Content.Parts[0].Text != "hello" {
		t.Fatalf("expected hello text, got %#v", resp.Content.Parts[0])
	}
	if len(resp.FunctionCalls) != 1 || resp.FunctionCalls[0].ID != "call_weather" {
		t.Fatalf("expected call_weather function call, got %#v", resp.FunctionCalls)
	}
}

func TestOpenAIToolsFromGemini_NormalizesSchemaTypes(t *testing.T) {
	tools, err := openAIToolsFromGemini([]*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "read_soul",
			Parameters: &genai.Schema{
				Type:             genai.TypeObject,
				PropertyOrdering: []string{"target"},
				Properties: map[string]*genai.Schema{
					"target": {Type: genai.TypeString},
				},
				Required: []string{"target"},
			},
		}},
	}})
	if err != nil {
		t.Fatalf("openAIToolsFromGemini returned error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	fn, ok := tools[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("expected function map, got %T", tools[0]["function"])
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("expected parameters map, got %T", fn["parameters"])
	}
	if params["type"] != "object" {
		t.Fatalf("expected object type, got %#v", params["type"])
	}
	if _, exists := params["propertyOrdering"]; exists {
		t.Fatalf("expected propertyOrdering to be dropped, got %#v", params["propertyOrdering"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}
	target, ok := props["target"].(map[string]any)
	if !ok {
		t.Fatalf("expected target schema map, got %T", props["target"])
	}
	if target["type"] != "string" {
		t.Fatalf("expected string type, got %#v", target["type"])
	}
}

func TestAnthropicToolsFromGemini_NormalizesSchemaTypes(t *testing.T) {
	tools, err := anthropicToolsFromGemini([]*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "read_soul",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"depth": {Type: genai.TypeInteger},
				},
			},
		}},
	}})
	if err != nil {
		t.Fatalf("anthropicToolsFromGemini returned error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	schema, ok := tools[0]["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema map, got %T", tools[0]["input_schema"])
	}
	if schema["type"] != "object" {
		t.Fatalf("expected object type, got %#v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", schema["properties"])
	}
	depth, ok := props["depth"].(map[string]any)
	if !ok {
		t.Fatalf("expected depth schema map, got %T", props["depth"])
	}
	if depth["type"] != "integer" {
		t.Fatalf("expected integer type, got %#v", depth["type"])
	}
}

func TestOpenAIToolsFromGemini_EmptyObjectSchemaIncludesProperties(t *testing.T) {
	tools, err := openAIToolsFromGemini([]*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:       "read_soul",
			Parameters: &genai.Schema{Type: genai.TypeObject},
		}},
	}})
	if err != nil {
		t.Fatalf("openAIToolsFromGemini returned error: %v", err)
	}
	fn, ok := tools[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("expected function map, got %T", tools[0]["function"])
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("expected parameters map, got %T", fn["parameters"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}
	if len(props) != 0 {
		t.Fatalf("expected empty properties object, got %#v", props)
	}
}

func TestContentsToAnthropicMessages_GroupsConsecutiveToolResults(t *testing.T) {
	contents := []*genai.Content{
		{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "tool_a",
						Name: "tool_a",
						Args: map[string]any{"x": 1},
					},
				},
				{
					FunctionCall: &genai.FunctionCall{
						ID:   "tool_b",
						Name: "tool_b",
						Args: map[string]any{"y": 2},
					},
				},
			},
		},
		genai.NewContentFromFunctionResponse("tool_a", map[string]any{"ok": true}, genai.RoleUser),
		genai.NewContentFromFunctionResponse("tool_b", map[string]any{"ok": true}, genai.RoleUser),
	}

	messages, err := contentsToAnthropicMessages(contents)
	if err != nil {
		t.Fatalf("contentsToAnthropicMessages returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "assistant" {
		t.Fatalf("expected assistant message first, got %#v", messages[0])
	}
	if messages[1]["role"] != "user" {
		t.Fatalf("expected user tool-result message second, got %#v", messages[1])
	}
	content, ok := messages[1]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected user content blocks, got %T", messages[1]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected grouped tool_result blocks, got %d", len(content))
	}
	if content[0]["type"] != "tool_result" || content[1]["type"] != "tool_result" {
		t.Fatalf("expected only tool_result blocks, got %#v", content)
	}
}

func TestReadCodexAccessToken_UsesOverrideFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(map[string]any{
		"tokens": map[string]any{
			"access_token": "abc123",
		},
	})
	if err != nil {
		t.Fatalf("marshal auth json: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	t.Setenv(codexAuthFileEnv, path)
	t.Setenv("OPENAI_API_KEY", "")

	token, err := readCodexAccessToken()
	if err != nil {
		t.Fatalf("readCodexAccessToken returned error: %v", err)
	}
	if token != "abc123" {
		t.Fatalf("expected abc123, got %q", token)
	}
}

func TestReadCodexOpenAIToken_PrefersAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(map[string]any{
		"OPENAI_API_KEY": "sk-codex-generated",
		"tokens": map[string]any{
			"access_token": "abc123",
		},
	})
	if err != nil {
		t.Fatalf("marshal auth json: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	t.Setenv(codexAuthFileEnv, path)
	t.Setenv("OPENAI_API_KEY", "")

	token, err := readCodexOpenAIToken()
	if err != nil {
		t.Fatalf("readCodexOpenAIToken returned error: %v", err)
	}
	if token != "sk-codex-generated" {
		t.Fatalf("expected sk-codex-generated, got %q", token)
	}
}

func TestReadCodexOpenAIToken_PrefersEnvAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(map[string]any{
		"OPENAI_API_KEY": "sk-codex-generated",
		"tokens": map[string]any{
			"access_token": "abc123",
		},
	})
	if err != nil {
		t.Fatalf("marshal auth json: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	t.Setenv(codexAuthFileEnv, path)
	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	token, err := readCodexOpenAIToken()
	if err != nil {
		t.Fatalf("readCodexOpenAIToken returned error: %v", err)
	}
	if token != "sk-from-env" {
		t.Fatalf("expected sk-from-env, got %q", token)
	}
}
