package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// HTTPRequestInput defines the input for the HTTP request tool.
type HTTPRequestInput struct {
	URL      string            `json:"url" description:"The full URL to request" required:"true"`
	Method   string            `json:"method,omitempty" description:"HTTP method" enum:"GET,POST,PUT,DELETE,PATCH"`
	Headers  map[string]string `json:"headers,omitempty" description:"Key-value pairs for HTTP headers (e.g., Authorization, User-Agent, Content-Type)"`
	JSONBody map[string]any    `json:"json_body,omitempty" description:"JSON payload for POST/PUT/PATCH requests. Automatically sets Content-Type: application/json and serializes to JSON."`
	DataBody string            `json:"data_body,omitempty" description:"Raw string body for text/plain, form data, or other content types. Use this instead of json_body for non-JSON payloads."`
	Params   map[string]string `json:"params,omitempty" description:"Query parameters to append to the URL. Automatically URL-encoded."`
	Timeout  int               `json:"timeout,omitempty" description:"Timeout in seconds (default 30, max 120)"`
}

// HTTPTools provides HTTP request tools.
type HTTPTools struct {
	// No state needed
}

// NewHTTPTools creates a new HTTPTools instance.
func NewHTTPTools() *HTTPTools {
	return &HTTPTools{}
}

// HttpRequestTool sends an HTTP request and returns the response.
func (h *HTTPTools) HttpRequestTool(ctx context.Context, input HTTPRequestInput) (*loop.ToolResult, error) {
	if input.URL == "" {
		return loop.NewErrorResult("url is required"), nil
	}

	// Default method to GET
	method := strings.ToUpper(input.Method)
	if method == "" {
		method = "GET"
	}

	// Validate method
	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true}
	if !validMethods[method] {
		return loop.NewErrorResult(fmt.Sprintf("invalid method: %s", method)), nil
	}

	// Parse and validate URL
	parsedURL, err := url.Parse(input.URL)
	if err != nil {
		return h.errorResponse(input.URL, fmt.Sprintf("invalid URL: %v", err)), nil
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return h.errorResponse(input.URL, "URL must use http or https scheme"), nil
	}

	// Add query parameters
	if len(input.Params) > 0 {
		q := parsedURL.Query()
		for k, v := range input.Params {
			q.Set(k, v)
		}
		parsedURL.RawQuery = q.Encode()
	}
	finalURL := parsedURL.String()

	// Prepare request body
	var body io.Reader
	var contentType string

	if input.JSONBody != nil {
		jsonData, err := json.Marshal(input.JSONBody)
		if err != nil {
			return h.errorResponse(finalURL, fmt.Sprintf("failed to serialize json_body: %v", err)), nil
		}
		body = bytes.NewReader(jsonData)
		contentType = "application/json"
	} else if input.DataBody != "" {
		body = strings.NewReader(input.DataBody)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, finalURL, body)
	if err != nil {
		return h.errorResponse(finalURL, fmt.Sprintf("failed to create request: %v", err)), nil
	}

	// Set default headers
	req.Header.Set("User-Agent", "GoWildAgent/1.0")
	req.Header.Set("Accept", "*/*")

	// Set content type if we have a JSON body
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Set custom headers (these can override defaults)
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	// Set timeout
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 120 {
		timeout = 120
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		// Don't follow redirects automatically so we can report the final URL
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		// Categorize the error
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "no such host") {
			errorMsg = fmt.Sprintf("DNS error: %v", err)
		} else if strings.Contains(errorMsg, "connection refused") {
			errorMsg = fmt.Sprintf("Connection refused: %v", err)
		} else if strings.Contains(errorMsg, "timeout") || strings.Contains(errorMsg, "deadline exceeded") {
			errorMsg = fmt.Sprintf("Request timeout after %ds: %v", timeout, err)
		} else if strings.Contains(errorMsg, "certificate") {
			errorMsg = fmt.Sprintf("TLS/SSL error: %v", err)
		}
		return h.errorResponse(finalURL, errorMsg), nil
	}
	defer resp.Body.Close()

	// Read response body with size limit (10MB)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return h.errorResponse(finalURL, fmt.Sprintf("failed to read response body: %v", err)), nil
	}

	bodyStr := string(bodyBytes)

	// Build response headers map
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) == 1 {
			respHeaders[strings.ToLower(k)] = v[0]
		} else {
			respHeaders[strings.ToLower(k)] = strings.Join(v, ", ")
		}
	}

	// Try to parse body as JSON
	var jsonBody any
	if strings.Contains(respHeaders["content-type"], "application/json") ||
		strings.HasPrefix(strings.TrimSpace(bodyStr), "{") ||
		strings.HasPrefix(strings.TrimSpace(bodyStr), "[") {
		if err := json.Unmarshal(bodyBytes, &jsonBody); err == nil {
			// Successfully parsed as JSON
		}
	}

	result := map[string]any{
		"status_code": resp.StatusCode,
		"status":      resp.Status,
		"headers":     respHeaders,
		"url":         resp.Request.URL.String(), // Final URL after redirects
		"error":       nil,
	}

	// Return either parsed JSON or string body — never both.
	// Including both doubles context usage for JSON responses.
	if jsonBody != nil {
		result["json"] = jsonBody
	} else {
		displayBody := bodyStr
		if len(displayBody) > 100*1024 {
			result["truncated"] = true
			result["full_length"] = len(bodyStr)
			displayBody = displayBody[:100*1024] + "\n... [truncated, " + fmt.Sprintf("%d", len(bodyStr)-100*1024) + " more bytes]"
		}
		result["body"] = displayBody
	}

	return loop.NewSuccessResult(result), nil
}

// errorResponse creates a structured error response.
func (h *HTTPTools) errorResponse(url string, errorMsg string) *loop.ToolResult {
	return loop.NewSuccessResult(map[string]any{
		"status_code": 0,
		"status":      "",
		"headers":     map[string]string{},
		"body":        "",
		"url":         url,
		"error":       errorMsg,
	})
}

// DescribeTool implements ToolProvider for tool descriptions.
func (h *HTTPTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"http_request": `Send HTTP requests to interact with APIs and web services.

METHODS: GET (default), POST, PUT, DELETE, PATCH, HEAD, OPTIONS

REQUEST OPTIONS:
- headers: Custom HTTP headers (e.g., {"Authorization": "Bearer sk_..."})
- json_body: Dict/object automatically serialized to JSON with Content-Type: application/json
- data_body: Raw string body for non-JSON payloads (form data, plain text, XML)
- params: Query parameters auto-appended and URL-encoded (e.g., {"id": "123"} → ?id=123)
- timeout: Request timeout in seconds (default 30, max 120)

RESPONSE STRUCTURE:
{
  "status_code": 200,           // HTTP status code for decision logic
  "status": "200 OK",           // Human-readable status
  "headers": {...},             // Response headers (lowercase keys)
  "body": "...",                // Raw response body (only for non-JSON responses)
  "json": {...},                // Parsed JSON object (only for JSON responses)
  "url": "https://...",         // Final URL after redirects
  "error": null                 // Error message if request failed before reaching server
}
Note: "body" and "json" are mutually exclusive — JSON responses return "json", others return "body".

STATUS CODE HANDLING:
- 200-299: Success
- 401/403: Auth failed - check API key/token
- 429: Rate limited - check Retry-After header
- 500+: Server error - retry later
- status_code=0 with error: Network/DNS/timeout failure`,
	}
	return descriptions[name]
}
