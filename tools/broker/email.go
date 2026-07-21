package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// EmailTools proxies email operations through the broker API.
type EmailTools struct {
	client *Client
}

// NewEmailTools creates broker-backed email tools.
func NewEmailTools(client *Client) *EmailTools {
	return &EmailTools{client: client}
}

func (e *EmailTools) ListEmailsTool(ctx context.Context, input tools.ListEmailsInput) (*loop.ToolResult, error) {
	result, err := e.client.Post(ctx, "/broker/v1/email/list", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (e *EmailTools) ReadEmailTool(ctx context.Context, input tools.ReadEmailInput) (*loop.ToolResult, error) {
	result, err := e.client.Post(ctx, "/broker/v1/email/read", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (e *EmailTools) SendEmailTool(ctx context.Context, input tools.SendEmailInput) (*loop.ToolResult, error) {
	result, err := e.client.Post(ctx, "/broker/v1/email/send", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (e *EmailTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"list_emails": "List messages, threads, or inbox details",
		"read_email":  "Read a specific message or thread",
		"send_email":  "Send, reply, forward, or update labels on an email",
	}
	return descriptions[name]
}
