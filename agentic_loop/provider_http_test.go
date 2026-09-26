package gowild_agentic_loop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"
)

// capturedRequest is one request a fake provider received.
type capturedRequest struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

// fakeProvider answers every request with one status and body, and records
// what it was sent.
type fakeProvider struct {
	mu       sync.Mutex
	status   int
	reply    string
	requests []capturedRequest
}

func startFakeProvider(t *testing.T, status int, reply string) (*fakeProvider, *httptest.Server) {
	t.Helper()
	fake := &fakeProvider{status: status, reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, capturedRequest{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		status, reply := fake.status, fake.reply
		fake.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return fake, srv
}

func (f *fakeProvider) last(t *testing.T) capturedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fake provider received no request")
	}
	return f.requests[len(f.requests)-1]
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func weatherTool() []*genai.Tool {
	return []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		nil,
		{
			Name:        "get_weather",
			Description: "weather lookup",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{"city": {Type: genai.TypeString}},
				Required:   []string{"city"},
			},
		},
	}}}
}

func toolHistory() []*genai.Content {
	return []*genai.Content{
		{Role: string(genai.RoleUser), Parts: []*genai.Part{
			nil,
			{Text: "What is in this picture and the weather?"},
			{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1, 2, 3}}},
		}},
		{Role: string(genai.RoleModel), Parts: []*genai.Part{
			{Text: "hidden", Thought: true},
			{Text: "Checking."},
			{FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]any{"city": "SF"}}},
		}},
		genai.NewContentFromFunctionResponse("get_weather", map[string]any{"temp_f": 68}, genai.RoleUser),
	}
}

func TestAnthropicClientGenerateContentRoundTrip(t *testing.T) {
	fake, srv := startFakeProvider(t, http.StatusOK, `{
		"content":[
			{"type":"thinking","text":"ignored"},
			{"type":"text","text":"Sunny, 68F."},
			{"type":"text","text":""},
			{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"city":"LA"}},
			{"type":"tool_use","id":"tu_2","name":"no_args","input":null}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":7,"output_tokens":3}
	}`)

	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Provider:   ProviderAnthropic,
		Model:      "claude-test",
		APIKey:     "anthropic-key",
		BaseURL:    srv.URL + "/v1/",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	defer client.Close()

	temp := float32(0.5)
	resp, err := client.GenerateContent(context.Background(), toolHistory(), &GenerateContentConfig{
		SystemInstruction: "be brief",
		MaxOutputTokens:   256,
		Temperature:       &temp,
		ThinkingBudget:    1024,
		Tools:             weatherTool(),
	})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}

	req := fake.last(t)
	if req.Path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages without a doubled /v1", req.Path)
	}
	if req.Header.Get("x-api-key") != "anthropic-key" || req.Header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("headers = %v", req.Header)
	}
	if req.Body["model"] != "claude-test" || req.Body["system"] != "be brief" ||
		req.Body["max_tokens"] != float64(256) || req.Body["temperature"] != 0.5 {
		t.Errorf("request body = %#v", req.Body)
	}
	if got := jsonOf(t, req.Body["thinking"]); got != `{"budget_tokens":1024,"type":"enabled"}` {
		t.Errorf("thinking = %s", got)
	}
	if got := jsonOf(t, req.Body["tools"]); got != `[{"description":"weather lookup","input_schema":{"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"},"name":"get_weather"}]` {
		t.Errorf("tools = %s", got)
	}
	wantMessages := `[` +
		`{"content":[{"text":"What is in this picture and the weather?","type":"text"},{"source":{"data":"AQID","media_type":"image/png","type":"base64"},"type":"image"}],"role":"user"},` +
		`{"content":[{"text":"Checking.","type":"text"},{"id":"call_1_get_weather","input":{"city":"SF"},"name":"get_weather","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"content":"{\"temp_f\":68}","tool_use_id":"call_1_get_weather","type":"tool_result"}],"role":"user"}]`
	if got := jsonOf(t, req.Body["messages"]); got != wantMessages {
		t.Errorf("messages =\n%s\nwant\n%s", got, wantMessages)
	}

	if got := ExtractText(resp.Content); got != "Sunny, 68F." {
		t.Errorf("text = %q", got)
	}
	if resp.FinishReason != "tool_use" {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if got := jsonOf(t, resp.FunctionCalls); got != `[{"id":"tu_1","args":{"city":"LA"},"name":"get_weather"},{"id":"tu_2","name":"no_args"}]` {
		t.Errorf("function calls = %s", got)
	}
	if resp.Usage == nil || *resp.Usage != (ModelUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}) {
		t.Errorf("usage = %#v", resp.Usage)
	}
}

func TestAnthropicClientModelSelectionAndStreamingFallback(t *testing.T) {
	fake, srv := startFakeProvider(t, http.StatusOK, `{"content":[{"type":"text","text":"hi there"}],"stop_reason":"end_turn"}`)
	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Model: "claude-first", APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	client.SetModel("claude-second")
	if client.GetModel() != "claude-second" {
		t.Errorf("GetModel = %q", client.GetModel())
	}

	var deltas []string
	resp, err := client.GenerateContentStreaming(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil,
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("GenerateContentStreaming: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "hi there" {
		t.Errorf("deltas = %q, want the whole text once", deltas)
	}
	if resp.Usage != nil {
		t.Errorf("usage = %#v, want nil when the reply reports none", resp.Usage)
	}
	req := fake.last(t)
	if req.Body["model"] != "claude-second" || req.Body["max_tokens"] != float64(4096) {
		t.Errorf("body = %#v, want SetModel's model and the default token cap", req.Body)
	}
	if _, ok := req.Body["system"]; ok {
		t.Errorf("system sent without a system instruction")
	}

	if _, err := client.GenerateContent(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		&GenerateContentConfig{Model: " claude-override "}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Body["model"]; got != "claude-override" {
		t.Errorf("model = %v, want the per-request override", got)
	}

	client.SetModel("")
	if _, err := client.GenerateContent(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Body["model"]; got != DefaultAnthropicModel {
		t.Errorf("model = %v, want the default when none is set", got)
	}
}

func TestAnthropicClientErrors(t *testing.T) {
	contents := []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}

	t.Run("no api key", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		_, err := NewProviderClient(context.Background(), ProviderClientConfig{Provider: ProviderAnthropic})
		if err == nil || err.Error() != "ANTHROPIC_API_KEY not set" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("key and base url from env", func(t *testing.T) {
		fake, srv := startFakeProvider(t, http.StatusOK, `{"content":[]}`)
		t.Setenv("ANTHROPIC_API_KEY", "env-key")
		t.Setenv("ANTHROPIC_BASE_URL", srv.URL+"/")
		client, err := NewProviderClient(context.Background(), ProviderClientConfig{Provider: ProviderAnthropic})
		if err != nil {
			t.Fatalf("NewProviderClient: %v", err)
		}
		if client.GetModel() != DefaultAnthropicModel {
			t.Errorf("model = %q", client.GetModel())
		}
		if _, err := client.GenerateContent(context.Background(), contents, nil); err != nil {
			t.Fatalf("GenerateContent: %v", err)
		}
		if got := fake.last(t).Header.Get("x-api-key"); got != "env-key" {
			t.Errorf("x-api-key = %q", got)
		}
	})

	cases := []struct {
		name    string
		status  int
		reply   string
		config  *GenerateContentConfig
		wantErr string
	}{
		{"structured output", http.StatusOK, `{}`, &GenerateContentConfig{ResponseMIMEType: "application/json"},
			"anthropic client does not support structured output options"},
		{"error object", http.StatusTooManyRequests, `{"error":{"type":"rate_limit","message":"slow down"}}`, nil,
			"llm request failed with status 429: slow down"},
		{"error string", http.StatusBadRequest, `{"error":"bad model"}`, nil,
			"llm request failed with status 400: bad model"},
		{"plain body", http.StatusBadGateway, `upstream gone`, nil,
			"llm request failed with status 502: upstream gone"},
		{"undecodable reply", http.StatusOK, `{not json`, nil,
			"failed to decode provider response: invalid character 'n' looking for beginning of object key string"},
		{"non-object tool input", http.StatusOK, `{"content":[{"type":"tool_use","name":"t","input":"text"}]}`, nil,
			"failed to decode tool args for t: json: cannot unmarshal string into Go value of type map[string]interface {}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := startFakeProvider(t, tc.status, tc.reply)
			client, err := NewProviderClient(context.Background(), ProviderClientConfig{
				Provider: ProviderAnthropic, APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatalf("NewProviderClient: %v", err)
			}
			_, err = client.GenerateContent(context.Background(), contents, tc.config)
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}

	t.Run("transport failure", func(t *testing.T) {
		_, srv := startFakeProvider(t, http.StatusOK, `{}`)
		client, err := NewProviderClient(context.Background(), ProviderClientConfig{
			Provider: ProviderAnthropic, APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
		})
		if err != nil {
			t.Fatalf("NewProviderClient: %v", err)
		}
		srv.Close()
		_, err = client.GenerateContent(context.Background(), contents, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "anthropic request failed: ") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestAnthropicMessagesDropEmptyTurnsAndSynthesizeOrphanIDs(t *testing.T) {
	messages, err := anthropicMessagesFromContents([]*genai.Content{
		nil,
		{Role: string(genai.RoleUser), Parts: []*genai.Part{{InlineData: &genai.Blob{MIMEType: "image/png"}}}},
		{Role: string(genai.RoleModel), Parts: []*genai.Part{nil, {Text: "only thought", Thought: true}}},
		{Role: string(genai.RoleModel), Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: " given ", Name: "a"}}}},
		{Role: string(genai.RoleUser), Parts: []*genai.Part{
			nil,
			{FunctionResponse: &genai.FunctionResponse{Name: "b", Response: map[string]any{"ok": true}}},
			{FunctionResponse: &genai.FunctionResponse{Name: "a", Response: map[string]any{"ok": false}}},
		}},
		genai.NewContentFromText("next", genai.RoleUser),
	})
	if err != nil {
		t.Fatalf("anthropicMessagesFromContents: %v", err)
	}
	want := `[` +
		`{"content":[{"id":"given","input":null,"name":"a","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"content":"{\"ok\":true}","tool_use_id":"given","type":"tool_result"},{"content":"{\"ok\":false}","tool_use_id":"call_4_a","type":"tool_result"}],"role":"user"},` +
		`{"content":[{"text":"next","type":"text"}],"role":"user"}]`
	if got := jsonOf(t, messages); got != want {
		t.Errorf("messages =\n%s\nwant\n%s", got, want)
	}
}

func TestOpenAIChatClientRoundTrip(t *testing.T) {
	fake, srv := startFakeProvider(t, http.StatusOK, `{
		"choices":[{
			"finish_reason":"tool_calls",
			"message":{
				"content":[{"type":"text","text":"Sunny"},{"type":"text","text":""},{"type":"text","text":", 68F."}],
				"tool_calls":[
					{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"LA\"}"}},
					{"id":"c2","type":"function","function":{"name":"no_args","arguments":""}}
				]
			}
		}],
		"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}
	}`)
	t.Setenv("OPENAI_ORG_ID", "org-1")
	t.Setenv("OPENAI_PROJECT_ID", "proj-1")

	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Provider: ProviderOpenAI, Model: "gpt-test", APIKey: "sk-test", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	defer client.Close()

	temp := float32(0.2)
	resp, err := client.GenerateContent(context.Background(), toolHistory(), &GenerateContentConfig{
		SystemInstruction: "be brief",
		Temperature:       &temp,
		MaxOutputTokens:   99,
		Tools:             weatherTool(),
		ResponseSchema:    &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"a": {Type: genai.TypeString}}},
	})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}

	req := fake.last(t)
	if req.Path != "/v1/chat/completions" {
		t.Errorf("path = %q", req.Path)
	}
	if req.Header.Get("Authorization") != "Bearer sk-test" ||
		req.Header.Get("OpenAI-Organization") != "org-1" || req.Header.Get("OpenAI-Project") != "proj-1" {
		t.Errorf("headers = %v", req.Header)
	}
	if req.Body["model"] != "gpt-test" || req.Body["max_completion_tokens"] != float64(99) ||
		req.Body["reasoning_effort"] != nil || req.Body["tool_choice"] != "auto" {
		t.Errorf("body = %#v", req.Body)
	}
	if got := jsonOf(t, req.Body["temperature"]); got != "0.2" {
		t.Errorf("temperature = %s", got)
	}
	if got := jsonOf(t, req.Body["response_format"]); got != `{"json_schema":{"name":"response","schema":{"properties":{"a":{"type":"string"}},"type":"object"},"strict":true},"type":"json_schema"}` {
		t.Errorf("response_format = %s", got)
	}
	wantMessages := `[` +
		`{"content":"be brief","role":"system"},` +
		`{"content":[{"text":"What is in this picture and the weather?","type":"text"},{"image_url":{"url":"data:image/png;base64,AQID"},"type":"image_url"}],"role":"user"},` +
		`{"content":"Checking.","role":"assistant","tool_calls":[{"function":{"arguments":"{\"city\":\"SF\"}","name":"get_weather"},"id":"call_1_get_weather","type":"function"}]},` +
		`{"content":"{\"temp_f\":68}","role":"tool","tool_call_id":"call_1_get_weather"}]`
	if got := jsonOf(t, req.Body["messages"]); got != wantMessages {
		t.Errorf("messages =\n%s\nwant\n%s", got, wantMessages)
	}

	if got := ExtractText(resp.Content); got != "Sunny, 68F." {
		t.Errorf("text = %q", got)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("finish = %q", resp.FinishReason)
	}
	if got := jsonOf(t, resp.FunctionCalls); got != `[{"id":"c1","args":{"city":"LA"},"name":"get_weather"},{"id":"c2","name":"no_args"}]` {
		t.Errorf("function calls = %s", got)
	}
	if resp.Usage == nil || *resp.Usage != (ModelUsage{PromptTokens: 11, CompletionTokens: 5, TotalTokens: 16}) {
		t.Errorf("usage = %#v", resp.Usage)
	}
}

func TestOpenAIChatClientPlainTextAndJSONMode(t *testing.T) {
	fake, srv := startFakeProvider(t, http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"plain answer"}}]}`)
	t.Setenv("OPENAI_ORG_ID", "")
	t.Setenv("OPENAI_PROJECT_ID", "")
	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Model: "gpt-first", APIKey: "sk", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	client.SetModel("gpt-second")
	if client.GetModel() != "gpt-second" {
		t.Errorf("GetModel = %q", client.GetModel())
	}

	var deltas []string
	resp, err := client.GenerateContentStreaming(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		&GenerateContentConfig{ResponseMIMEType: "application/json", ThinkingBudget: 500},
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("GenerateContentStreaming: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "plain answer" {
		t.Errorf("deltas = %q", deltas)
	}
	if resp.Usage != nil {
		t.Errorf("usage = %#v, want nil", resp.Usage)
	}
	req := fake.last(t)
	if req.Body["model"] != "gpt-second" || req.Body["reasoning_effort"] != "low" {
		t.Errorf("body = %#v, want SetModel's model and low effort without tools", req.Body)
	}
	if got := jsonOf(t, req.Body["response_format"]); got != `{"type":"json_object"}` {
		t.Errorf("response_format = %s", got)
	}
	if req.Header.Get("OpenAI-Organization") != "" || req.Header.Get("OpenAI-Project") != "" {
		t.Errorf("org/project headers sent without env: %v", req.Header)
	}

	if _, err := client.GenerateContent(context.Background(),
		[]*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		&GenerateContentConfig{Model: "gpt-override"}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Body["model"]; got != "gpt-override" {
		t.Errorf("model = %v", got)
	}

	fake.mu.Lock()
	fake.reply = `{"choices":[]}`
	fake.mu.Unlock()
	client.SetModel("")
	resp, err = client.GenerateContent(context.Background(), []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if resp.Content != nil || resp.FunctionCalls != nil {
		t.Errorf("resp = %#v, want empty for no choices", resp)
	}
	if got := fake.last(t).Body["model"]; got != DefaultOpenAIModel {
		t.Errorf("model = %v, want default", got)
	}
}

func TestOpenAIResponsesClientRoundTrip(t *testing.T) {
	fake, srv := startFakeProvider(t, http.StatusOK, `{
		"status":"completed",
		"output":[
			{"type":"reasoning","id":"r1"},
			{"type":"message","content":[{"type":"output_text","text":"Sunny."},{"type":"refusal","text":"no"}]},
			{"type":"function_call","id":"fc_1","call_id":"","name":"get_weather","arguments":"{\"city\":\"LA\"}"}
		],
		"usage":{"input_tokens":20,"output_tokens":6,"total_tokens":26}
	}`)
	t.Setenv("OPENAI_ORG_ID", "org-2")
	t.Setenv("OPENAI_PROJECT_ID", "proj-2")
	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Provider: ProviderOpenAI, APIKey: "sk", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}

	history := append([]*genai.Content{{Role: "developer", Parts: []*genai.Part{{Text: "dev note"}}}}, toolHistory()...)
	resp, err := client.GenerateContent(context.Background(), history, &GenerateContentConfig{
		ThinkingBudget:   40000,
		Tools:            weatherTool(),
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}

	req := fake.last(t)
	if req.Path != "/v1/responses" {
		t.Errorf("path = %q, want the responses API for tools plus reasoning", req.Path)
	}
	if req.Header.Get("OpenAI-Organization") != "org-2" || req.Header.Get("OpenAI-Project") != "proj-2" {
		t.Errorf("headers = %v", req.Header)
	}
	if req.Body["model"] != DefaultOpenAIModel {
		t.Errorf("model = %v", req.Body["model"])
	}
	if got := jsonOf(t, req.Body["reasoning"]); got != `{"effort":"high"}` {
		t.Errorf("reasoning = %s", got)
	}
	if got := jsonOf(t, req.Body["text"]); got != `{"format":{"type":"json_object"}}` {
		t.Errorf("text = %s", got)
	}
	input, _ := req.Body["input"].([]any)
	if len(input) != 5 {
		t.Fatalf("input = %s, want 5 items", jsonOf(t, req.Body["input"]))
	}
	if got := jsonOf(t, input[0]); got != `{"content":[{"text":"dev note","type":"input_text"}],"role":"developer","type":"message"}` {
		t.Errorf("developer item = %s", got)
	}
	if got := jsonOf(t, input[1]); got != `{"content":[{"text":"What is in this picture and the weather?","type":"input_text"},{"image_url":"data:image/png;base64,AQID","type":"input_image"}],"role":"user","type":"message"}` {
		t.Errorf("user item = %s", got)
	}

	if got := ExtractText(resp.Content); got != "Sunny." {
		t.Errorf("text = %q", got)
	}
	if resp.FinishReason != "completed" {
		t.Errorf("finish = %q", resp.FinishReason)
	}
	if len(resp.FunctionCalls) != 1 || resp.FunctionCalls[0].ID != "fc_1" || resp.FunctionCalls[0].Args["city"] != "LA" {
		t.Errorf("function calls = %s, want the item id when call_id is empty", jsonOf(t, resp.FunctionCalls))
	}
	if resp.Usage == nil || *resp.Usage != (ModelUsage{PromptTokens: 20, CompletionTokens: 6, TotalTokens: 26}) {
		t.Errorf("usage = %#v", resp.Usage)
	}
}

func TestOpenAIResponsesRequestStructuredSchema(t *testing.T) {
	temp := float32(1)
	body, err := buildOpenAIResponsesRequest(" gpt-x ", []*genai.Content{
		{Role: "system", Parts: []*genai.Part{{Text: "sys"}}},
	}, &GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: 12,
		ResponseSchema:  &genai.Schema{Type: genai.TypeString},
	})
	if err != nil {
		t.Fatalf("buildOpenAIResponsesRequest: %v", err)
	}
	if got := jsonOf(t, body); got != `{"input":[{"content":[{"text":"sys","type":"input_text"}],"role":"system","type":"message"}],"max_output_tokens":12,"model":"gpt-x","temperature":1,"text":{"format":{"name":"response","schema":{"type":"string"},"strict":true,"type":"json_schema"}}}` {
		t.Errorf("body = %s", got)
	}

	if _, err := buildOpenAIResponsesRequest("gpt-x", []*genai.Content{
		{Role: string(genai.RoleModel), Parts: []*genai.Part{{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1}}}}},
	}, nil); err == nil || err.Error() != "openai responses does not support assistant image history items" {
		t.Errorf("assistant image err = %v", err)
	}
}

func TestOpenAIChatRequestJSONModeAndNilConfig(t *testing.T) {
	body, err := buildOpenAIChatRequest(" gpt-x ", []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil)
	if err != nil {
		t.Fatalf("buildOpenAIChatRequest: %v", err)
	}
	if got := jsonOf(t, body); got != `{"messages":[{"content":[{"text":"hi","type":"text"}],"role":"user"}],"model":"gpt-x"}` {
		t.Errorf("body = %s", got)
	}

	temp := float32(0)
	body, err = buildOpenAIChatRequest("gpt-x", nil, &GenerateContentConfig{
		Temperature: &temp, MaxOutputTokens: 5, ThinkingBudget: 9000,
		Tools: weatherTool(), ResponseMIMEType: "application/json",
	})
	if err != nil {
		t.Fatalf("buildOpenAIChatRequest: %v", err)
	}
	if body["reasoning_effort"] != "medium" || body["max_completion_tokens"] != int32(5) || body["tool_choice"] != "auto" {
		t.Errorf("body = %#v", body)
	}
	if got := jsonOf(t, body["response_format"]); got != `{"type":"json_object"}` {
		t.Errorf("response_format = %s", got)
	}
}

func TestOpenAIClientErrors(t *testing.T) {
	contents := []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}
	responsesConfig := &GenerateContentConfig{ThinkingBudget: 100, Tools: weatherTool()}

	t.Run("no api key", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		_, err := NewProviderClient(context.Background(), ProviderClientConfig{Provider: ProviderOpenAI})
		if err == nil || err.Error() != "OPENAI_API_KEY not set" {
			t.Errorf("err = %v", err)
		}
	})

	cases := []struct {
		name    string
		status  int
		reply   string
		config  *GenerateContentConfig
		wantErr string
	}{
		{"chat http error", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, nil,
			"llm request failed with status 401: bad key"},
		{"chat bad tool args", http.StatusOK, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"f","arguments":"{"}}]}}]}`, nil,
			"failed to decode tool args for f: unexpected end of JSON input"},
		{"responses http error", http.StatusServiceUnavailable, ``, responsesConfig,
			"llm request failed with status 503"},
		{"responses bad tool args", http.StatusOK, `{"output":[{"type":"function_call","name":"g","arguments":"nope"}]}`, responsesConfig,
			"failed to decode tool args for g: invalid character 'o' in literal null (expecting 'u')"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := startFakeProvider(t, tc.status, tc.reply)
			client, err := NewProviderClient(context.Background(), ProviderClientConfig{
				Provider: ProviderOpenAI, APIKey: "sk", BaseURL: srv.URL, HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatalf("NewProviderClient: %v", err)
			}
			_, err = client.GenerateContent(context.Background(), contents, tc.config)
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}

	for _, cfg := range []*GenerateContentConfig{nil, responsesConfig} {
		_, srv := startFakeProvider(t, http.StatusOK, `{}`)
		client, err := NewProviderClient(context.Background(), ProviderClientConfig{
			Provider: ProviderOpenAI, APIKey: "sk", BaseURL: srv.URL, HTTPClient: srv.Client(),
		})
		if err != nil {
			t.Fatalf("NewProviderClient: %v", err)
		}
		srv.Close()
		_, err = client.GenerateContent(context.Background(), contents, cfg)
		if err == nil || !strings.Contains(err.Error(), "request failed: ") || !strings.HasPrefix(err.Error(), "openai ") {
			t.Errorf("transport err = %v", err)
		}
	}
}

func TestOpenAICodexOAuthToken(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		return path
	}
	t.Setenv("OPENAI_API_KEY", "")

	fake, srv := startFakeProvider(t, http.StatusOK, `{"choices":[]}`)
	t.Setenv(codexAuthFileEnv, write("ok.json", `{"tokens":{"access_token":" oauth-token "}}`))
	client, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Provider: ProviderOpenAI, OpenAIAuthMode: OpenAIAuthModeCodexOAuth, BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewProviderClient: %v", err)
	}
	if _, err := client.GenerateContent(context.Background(), []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}, nil); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if got := fake.last(t).Header.Get("Authorization"); got != "Bearer oauth-token" {
		t.Errorf("Authorization = %q", got)
	}

	emptyKey := write("empty-key.json", `{"OPENAI_API_KEY":"  ","tokens":{"access_token":"fallback"}}`)
	t.Setenv(codexAuthFileEnv, emptyKey)
	if tok, err := readCodexOpenAIToken(); err != nil || tok != "fallback" {
		t.Errorf("blank api key: token = %q, err = %v; want the access token", tok, err)
	}

	neither := write("neither.json", `{"tokens":{}}`)
	t.Setenv(codexAuthFileEnv, neither)
	if _, err := readCodexOpenAIToken(); err == nil || err.Error() != `codex auth file "`+neither+`" does not contain OPENAI_API_KEY or access_token` {
		t.Errorf("neither err = %v", err)
	}
	if _, err := readCodexAccessToken(); err == nil || err.Error() != `codex auth file "`+neither+`" does not contain an access_token` {
		t.Errorf("access token err = %v", err)
	}

	bad := write("bad.json", `{`)
	t.Setenv(codexAuthFileEnv, bad)
	if _, err := readCodexOpenAIToken(); err == nil || !strings.HasPrefix(err.Error(), `failed to decode codex auth file "`+bad+`"`) {
		t.Errorf("bad json err = %v", err)
	}
	if _, err := readCodexAccessToken(); err == nil || !strings.HasPrefix(err.Error(), `failed to decode codex auth file`) {
		t.Errorf("access token bad json err = %v", err)
	}

	missing := filepath.Join(dir, "missing.json")
	t.Setenv(codexAuthFileEnv, missing)
	_, err = NewProviderClient(context.Background(), ProviderClientConfig{Provider: ProviderOpenAI, OpenAIAuthMode: OpenAIAuthModeCodexOAuth})
	if err == nil || !strings.HasPrefix(err.Error(), `failed to read codex auth file "`+missing+`"`) {
		t.Errorf("missing file err = %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(codexAuthFileEnv, "")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"tokens":{"access_token":"home-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := readCodexAccessToken(); err != nil || tok != "home-token" {
		t.Errorf("home auth file: token = %q, err = %v", tok, err)
	}
}

func TestProviderHTTPErrorNamesTheProviderByHost(t *testing.T) {
	for host, want := range map[string]string{
		"api.openai.com":    "openai request failed with status 500: boom",
		"api.anthropic.com": "anthropic request failed with status 500: boom",
	} {
		req, _ := http.NewRequest(http.MethodPost, "https://"+host+"/v1/x", nil)
		err := providerHTTPError(&http.Response{StatusCode: 500, Request: req}, []byte(`{"error":{"message":"boom"}}`))
		if err.Error() != want {
			t.Errorf("%s: err = %q, want %q", host, err, want)
		}
	}
}

func TestDecodeProviderResponseEmptyBodyAndNilTarget(t *testing.T) {
	var out map[string]any
	if err := decodeProviderResponse(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, &out); err != nil || out != nil {
		t.Errorf("empty body: out = %v, err = %v", out, err)
	}
	if err := decodeProviderResponse(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"a":1}`))}, nil); err != nil {
		t.Errorf("nil target: err = %v", err)
	}
	if err := decodeProviderResponse(&http.Response{StatusCode: 200, Body: io.NopCloser(failingReader{})}, &out); err == nil || err.Error() != "failed to read provider response: read failed" {
		t.Errorf("read failure: err = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errReadFailed }

var errReadFailed = errors.New("read failed")

func TestJoinProviderURL(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"", "/v1/messages", "/v1/messages"},
		{" https://proxy/v1/ ", "/v1/messages", "https://proxy/v1/messages"},
		{"https://proxy", "/v1/messages", "https://proxy/v1/messages"},
		{"https://proxy/api", "/v1/chat", "https://proxy/api/v1/chat"},
	}
	for _, tc := range cases {
		if got := joinProviderURL(tc.base, tc.path); got != tc.want {
			t.Errorf("joinProviderURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestProviderSchemaKeepsEveryConstraint(t *testing.T) {
	one, two := int64(1), int64(2)
	lo, hi := 0.5, 9.5
	nullable := true
	schema := &genai.Schema{
		Type:             genai.TypeObject,
		Title:            "T",
		Description:      "d",
		Default:          "x",
		Example:          "ex",
		Nullable:         &nullable,
		PropertyOrdering: []string{"s"},
		MinProperties:    &one,
		MaxProperties:    &two,
		AnyOf:            []*genai.Schema{nil, {Type: genai.TypeString}},
		Properties: map[string]*genai.Schema{
			"s": {Type: genai.TypeString, Format: "date", Pattern: "^a", MinLength: &one, MaxLength: &two, Enum: []string{"a", "b"}},
			"n": {Type: genai.TypeNumber, Minimum: &lo, Maximum: &hi},
			"l": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeInteger}, MinItems: &one, MaxItems: &two},
		},
	}
	keep := providerSchemaNode(schema, providerSchemaOptions{})
	if got := jsonOf(t, keep); got != `{"anyOf":[{"type":"STRING"}],"default":"x","description":"d","example":"ex","maxProperties":2,"minProperties":1,"nullable":true,"properties":{"l":{"items":{"type":"INTEGER"},"maxItems":2,"minItems":1,"type":"ARRAY"},"n":{"maximum":9.5,"minimum":0.5,"type":"NUMBER"},"s":{"enum":["a","b"],"format":"date","maxLength":2,"minLength":1,"pattern":"^a","type":"STRING"}},"propertyOrdering":["s"],"title":"T","type":"OBJECT"}` {
		t.Errorf("schema = %s", got)
	}
	openAI, err := openAISchemaToMap(schema)
	if err != nil {
		t.Fatalf("openAISchemaToMap: %v", err)
	}
	for _, dropped := range []string{"example", "nullable", "propertyOrdering"} {
		if _, ok := openAI[dropped]; ok {
			t.Errorf("openai schema kept %q", dropped)
		}
	}
	if openAI["type"] != "object" {
		t.Errorf("openai type = %v, want lowercase", openAI["type"])
	}
	if m, err := providerSchemaToMap(nil, providerSchemaOptions{}); m != nil || err != nil {
		t.Errorf("nil schema = %v, %v", m, err)
	}
}
