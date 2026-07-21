package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

const agentMailBaseURL = "https://api.agentmail.to/v0"

// EmailTools provides email tools using AgentMail API.
// Each agent has a single inbox configured via the -email-inbox flag.
type EmailTools struct {
	apiKey  string
	inboxID string
	baseURL string
	client  *http.Client
}

// NewEmailTools creates a new EmailTools instance.
// The inboxID is the agent's configured inbox from the database.
func NewEmailTools(apiKey, inboxID string) *EmailTools {
	return &EmailTools{
		apiKey:  apiKey,
		inboxID: inboxID,
		baseURL: agentMailBaseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GetInboxID returns the configured inbox ID.
func (e *EmailTools) GetInboxID() string {
	return e.inboxID
}

// --- API Request Helpers ---

func (e *EmailTools) doRequest(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, e.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			// If not JSON, return raw body as string
			return map[string]any{
				"status_code": resp.StatusCode,
				"body":        string(respBody),
			}, nil
		}
	}

	if result == nil {
		result = make(map[string]any)
	}
	result["status_code"] = resp.StatusCode

	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	return result, nil
}

func (e *EmailTools) doRequestWithQuery(ctx context.Context, method, path string, params url.Values) (map[string]any, error) {
	fullPath := path
	if len(params) > 0 {
		fullPath = path + "?" + params.Encode()
	}
	return e.doRequest(ctx, method, fullPath, nil)
}

// --- Input Structs ---

// ListEmailsInput is the input for listing emails.
type ListEmailsInput struct {
	View        string `json:"view,omitempty" description:"What to list: 'messages' (default), 'threads', or 'inbox'" enum:"messages,threads,inbox"`
	Limit       int    `json:"limit,omitempty" description:"Maximum number of items to return (default 20)"`
	PageToken   string `json:"page_token,omitempty" description:"Token for pagination"`
	Labels      string `json:"labels,omitempty" description:"Filter by label (e.g., 'inbox', 'sent', 'spam')"`
	Before      string `json:"before,omitempty" description:"Filter before this timestamp (RFC3339)"`
	After       string `json:"after,omitempty" description:"Filter after this timestamp (RFC3339)"`
	Ascending   bool   `json:"ascending,omitempty" description:"Sort in ascending order by date"`
	IncludeSpam bool   `json:"include_spam,omitempty" description:"Include spam folder items"`
}

// ReadEmailInput is the input for reading a specific email or thread.
type ReadEmailInput struct {
	MessageID string `json:"message_id,omitempty" description:"The message ID to retrieve (provide this OR thread_id)"`
	ThreadID  string `json:"thread_id,omitempty" description:"The thread ID to retrieve (provide this OR message_id)"`
}

// SendEmailInput is the input for sending, replying, forwarding, or updating labels.
type SendEmailInput struct {
	Action       string            `json:"action,omitempty" description:"Action to perform: 'send' (default), 'reply', 'forward', 'update_labels'" enum:"send,reply,forward,update_labels"`
	To           []string          `json:"to,omitempty" description:"Recipient email addresses (required for send/forward)"`
	Subject      string            `json:"subject,omitempty" description:"Email subject line (required for send)"`
	Text         string            `json:"text,omitempty" description:"Plain text body"`
	HTML         string            `json:"html,omitempty" description:"HTML body (optional, use text for plain emails)"`
	CC           []string          `json:"cc,omitempty" description:"CC recipient email addresses"`
	BCC          []string          `json:"bcc,omitempty" description:"BCC recipient email addresses"`
	Headers      map[string]string `json:"headers,omitempty" description:"Custom email headers (send only)"`
	Labels       []string          `json:"labels,omitempty" description:"Labels to apply to sent message (send only)"`
	MessageID    string            `json:"message_id,omitempty" description:"Message ID (required for reply/forward/update_labels)"`
	ReplyAll     bool              `json:"reply_all,omitempty" description:"Reply to all recipients (reply only)"`
	AddLabels    []string          `json:"add_labels,omitempty" description:"Labels to add (update_labels only)"`
	RemoveLabels []string          `json:"remove_labels,omitempty" description:"Labels to remove (update_labels only)"`
}

// --- Tool Methods ---

// ListEmailsTool lists messages, threads, or inbox details.
func (e *EmailTools) ListEmailsTool(ctx context.Context, input ListEmailsInput) (*loop.ToolResult, error) {
	view := input.View
	if view == "" {
		view = "messages"
	}

	switch view {
	case "inbox":
		result, err := e.doRequest(ctx, "GET", "/inboxes/"+e.inboxID, nil)
		if err != nil {
			return loop.NewErrorResult(err.Error()), nil
		}
		return loop.NewSuccessResult(result), nil

	case "messages":
		params := e.buildListParams(input)
		result, err := e.doRequestWithQuery(ctx, "GET", "/inboxes/"+e.inboxID+"/messages", params)
		if err != nil {
			return loop.NewErrorResult(err.Error()), nil
		}
		return loop.NewSuccessResult(result), nil

	case "threads":
		params := e.buildListParams(input)
		result, err := e.doRequestWithQuery(ctx, "GET", "/inboxes/"+e.inboxID+"/threads", params)
		if err != nil {
			return loop.NewErrorResult(err.Error()), nil
		}
		return loop.NewSuccessResult(result), nil

	default:
		return loop.NewErrorResult(fmt.Sprintf("invalid view: %q (must be messages, threads, or inbox)", view)), nil
	}
}

// buildListParams converts filter fields to query parameters.
func (e *EmailTools) buildListParams(input ListEmailsInput) url.Values {
	params := url.Values{}
	if input.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", input.Limit))
	}
	if input.PageToken != "" {
		params.Set("page_token", input.PageToken)
	}
	if input.Labels != "" {
		params.Set("labels", input.Labels)
	}
	if input.Before != "" {
		params.Set("before", input.Before)
	}
	if input.After != "" {
		params.Set("after", input.After)
	}
	if input.Ascending {
		params.Set("ascending", "true")
	}
	if input.IncludeSpam {
		params.Set("include_spam", "true")
	}
	return params
}

// ReadEmailTool reads a specific message or thread.
func (e *EmailTools) ReadEmailTool(ctx context.Context, input ReadEmailInput) (*loop.ToolResult, error) {
	if input.MessageID == "" && input.ThreadID == "" {
		return loop.NewErrorResult("either message_id or thread_id is required"), nil
	}
	if input.MessageID != "" && input.ThreadID != "" {
		return loop.NewErrorResult("provide either message_id or thread_id, not both"), nil
	}

	if input.MessageID != "" {
		result, err := e.doRequest(ctx, "GET", "/inboxes/"+e.inboxID+"/messages/"+input.MessageID, nil)
		if err != nil {
			return loop.NewErrorResult(err.Error()), nil
		}
		return loop.NewSuccessResult(result), nil
	}

	result, err := e.doRequest(ctx, "GET", "/inboxes/"+e.inboxID+"/threads/"+input.ThreadID, nil)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// SendEmailTool sends, replies, forwards, or updates labels on an email.
func (e *EmailTools) SendEmailTool(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	action := input.Action
	if action == "" {
		action = "send"
	}

	switch action {
	case "send":
		return e.doSend(ctx, input)
	case "reply":
		return e.doReply(ctx, input)
	case "forward":
		return e.doForward(ctx, input)
	case "update_labels":
		return e.doUpdateLabels(ctx, input)
	default:
		return loop.NewErrorResult(fmt.Sprintf("invalid action: %q (must be send, reply, forward, or update_labels)", action)), nil
	}
}

func (e *EmailTools) doSend(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	if len(input.To) == 0 {
		return loop.NewErrorResult("at least one recipient (to) is required"), nil
	}
	if input.Subject == "" {
		return loop.NewErrorResult("subject is required"), nil
	}
	if input.Text == "" && input.HTML == "" {
		return loop.NewErrorResult("either text or html body is required"), nil
	}

	body := map[string]any{
		"to":      input.To,
		"subject": input.Subject,
	}
	if input.Text != "" {
		body["text"] = input.Text
	}
	if input.HTML != "" {
		body["html"] = input.HTML
	}
	if len(input.CC) > 0 {
		body["cc"] = input.CC
	}
	if len(input.BCC) > 0 {
		body["bcc"] = input.BCC
	}
	if len(input.Headers) > 0 {
		body["headers"] = input.Headers
	}
	if len(input.Labels) > 0 {
		body["labels"] = input.Labels
	}

	result, err := e.doRequest(ctx, "POST", "/inboxes/"+e.inboxID+"/messages/send", body)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (e *EmailTools) doReply(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	if input.MessageID == "" {
		return loop.NewErrorResult("message_id is required for reply"), nil
	}
	if input.Text == "" && input.HTML == "" {
		return loop.NewErrorResult("either text or html body is required"), nil
	}

	body := make(map[string]any)
	if input.Text != "" {
		body["text"] = input.Text
	}
	if input.HTML != "" {
		body["html"] = input.HTML
	}
	if len(input.CC) > 0 {
		body["cc"] = input.CC
	}
	if len(input.BCC) > 0 {
		body["bcc"] = input.BCC
	}

	endpoint := "/inboxes/" + e.inboxID + "/messages/" + input.MessageID + "/reply"
	if input.ReplyAll {
		endpoint = "/inboxes/" + e.inboxID + "/messages/" + input.MessageID + "/reply-all"
	}

	result, err := e.doRequest(ctx, "POST", endpoint, body)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (e *EmailTools) doForward(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	if input.MessageID == "" {
		return loop.NewErrorResult("message_id is required for forward"), nil
	}
	if len(input.To) == 0 {
		return loop.NewErrorResult("at least one recipient (to) is required"), nil
	}

	body := map[string]any{
		"to": input.To,
	}
	if input.Text != "" {
		body["text"] = input.Text
	}
	if len(input.CC) > 0 {
		body["cc"] = input.CC
	}
	if len(input.BCC) > 0 {
		body["bcc"] = input.BCC
	}

	result, err := e.doRequest(ctx, "POST", "/inboxes/"+e.inboxID+"/messages/"+input.MessageID+"/forward", body)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (e *EmailTools) doUpdateLabels(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	if input.MessageID == "" {
		return loop.NewErrorResult("message_id is required for update_labels"), nil
	}

	body := make(map[string]any)
	if len(input.AddLabels) > 0 {
		body["add_labels"] = input.AddLabels
	}
	if len(input.RemoveLabels) > 0 {
		body["remove_labels"] = input.RemoveLabels
	}

	result, err := e.doRequest(ctx, "PATCH", "/inboxes/"+e.inboxID+"/messages/"+input.MessageID, body)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (e *EmailTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"list_emails": `List emails, threads, or inbox details.

Set "view" to control what is listed:
- "messages" (default): List messages with filtering by labels, date range, pagination
- "threads": List conversation threads with the same filtering options
- "inbox": Get inbox metadata (email address, display name, creation date)

Filter options (messages/threads only):
- labels: Filter by label (inbox, sent, spam, etc.)
- before/after: Date range filtering (RFC3339 timestamps)
- ascending: Sort order
- include_spam: Include spam folder`,

		"read_email": `Read a specific email message or thread.

Provide exactly one of:
- message_id: Get a single message with full content (subject, body, headers, attachments metadata, thread ID)
- thread_id: Get a complete thread with all messages in chronological order`,

		"send_email": `Send, reply to, forward, or update labels on an email.

Set "action" to control what happens:
- "send" (default): Send a new email. Requires: to, subject, and text or html
- "reply": Reply to a message. Requires: message_id and text or html. Set reply_all to reply to all recipients
- "forward": Forward a message. Requires: message_id and to
- "update_labels": Add/remove labels on a message. Requires: message_id and add_labels or remove_labels

Common labels: inbox, sent, starred, important, spam, trash`,
	}
	return descriptions[name]
}
