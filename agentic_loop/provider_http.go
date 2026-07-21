package gowild_agentic_loop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"google.golang.org/genai"
)

type providerToolCall struct {
	ID   string
	Name string
}

func syntheticToolCallID(index int, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("call_%d_%s", index, name)
}

func consumePendingToolCall(pending *[]providerToolCall, name string) string {
	if pending == nil || len(*pending) == 0 {
		return ""
	}
	name = strings.TrimSpace(name)
	for i, call := range *pending {
		if strings.TrimSpace(call.Name) != name {
			continue
		}
		consumed := call.ID
		*pending = append((*pending)[:i], (*pending)[i+1:]...)
		return consumed
	}
	consumed := (*pending)[0].ID
	*pending = (*pending)[1:]
	return consumed
}

func isFunctionResponseContent(content *genai.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part != nil && part.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func decodeProviderResponse(resp *http.Response, out any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read provider response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return providerHTTPError(resp, body)
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode provider response: %w", err)
	}
	return nil
}

func providerHTTPError(resp *http.Response, body []byte) error {
	message := strings.TrimSpace(string(body))

	var parsed struct {
		Error any `json:"error"`
		Type  string
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		switch errVal := parsed.Error.(type) {
		case string:
			if strings.TrimSpace(errVal) != "" {
				message = strings.TrimSpace(errVal)
			}
		case map[string]any:
			if msg, ok := errVal["message"].(string); ok && strings.TrimSpace(msg) != "" {
				message = strings.TrimSpace(msg)
			}
		}
	}

	provider := ""
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		switch {
		case strings.Contains(resp.Request.URL.Host, "openai"):
			provider = LLMProviderOpenAI
		case strings.Contains(resp.Request.URL.Host, "anthropic"):
			provider = LLMProviderAnthropic
		}
	}
	if provider == "" {
		provider = "llm"
	}
	return &httpStatusError{
		Provider: provider,
		Code:     resp.StatusCode,
		Message:  message,
		Body:     strings.TrimSpace(string(body)),
	}
}
