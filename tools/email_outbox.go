package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// EmailOutbox intercepts outgoing email tools (send, reply, forward) and queues
// them for user approval unless all recipients are whitelisted.
// It registers a SendEmailTool method with the same name as EmailTools, so when added
// after EmailTools via AddTools, it overwrites the outgoing tool.
type EmailOutbox struct {
	email   *EmailTools
	service *data.AgentService
}

// NewEmailOutbox creates a new EmailOutbox that wraps an EmailTools instance.
func NewEmailOutbox(email *EmailTools, service *data.AgentService) *EmailOutbox {
	return &EmailOutbox{
		email:   email,
		service: service,
	}
}

// PendingCount returns the number of pending emails awaiting approval.
func (o *EmailOutbox) PendingCount(ctx context.Context) int {
	emails, err := o.service.GetPendingEmails(ctx)
	if err != nil {
		return 0
	}
	return len(emails)
}

// GetPending returns all pending emails.
func (o *EmailOutbox) GetPending(ctx context.Context) ([]*data.PendingEmail, error) {
	return o.service.GetPendingEmails(ctx)
}

// Approve sends a pending email and marks it as approved.
func (o *EmailOutbox) Approve(ctx context.Context, id string) (*data.PendingEmail, error) {
	pe, err := o.service.GetPendingEmailByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("email not found: %w", err)
	}
	if pe.Status != "pending" {
		return nil, fmt.Errorf("email is already %s", pe.Status)
	}

	var input SendEmailInput
	if err := json.Unmarshal([]byte(pe.RequestData), &input); err != nil {
		return nil, fmt.Errorf("failed to deserialize request: %w", err)
	}
	result, err := o.email.SendEmailTool(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to send email: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("email API error: %s", result.Error)
	}

	if err := o.service.UpdatePendingEmailStatus(ctx, id, "approved"); err != nil {
		return nil, fmt.Errorf("email sent but failed to update status: %w", err)
	}

	return pe, nil
}

// ApproveAll approves and sends all pending emails.
func (o *EmailOutbox) ApproveAll(ctx context.Context) ([]*data.PendingEmail, error) {
	pending, err := o.service.GetPendingEmails(ctx)
	if err != nil {
		return nil, err
	}

	var approved []*data.PendingEmail
	for _, pe := range pending {
		sent, err := o.Approve(ctx, pe.ID)
		if err != nil {
			return approved, fmt.Errorf("failed to approve email %s: %w", pe.ID, err)
		}
		approved = append(approved, sent)
	}
	return approved, nil
}

// Reject marks a pending email as rejected without sending.
func (o *EmailOutbox) Reject(ctx context.Context, id string) (*data.PendingEmail, error) {
	pe, err := o.service.GetPendingEmailByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("email not found: %w", err)
	}
	if pe.Status != "pending" {
		return nil, fmt.Errorf("email is already %s", pe.Status)
	}

	if err := o.service.UpdatePendingEmailStatus(ctx, id, "rejected"); err != nil {
		return nil, err
	}
	return pe, nil
}

// RejectAll rejects all pending emails.
func (o *EmailOutbox) RejectAll(ctx context.Context) ([]*data.PendingEmail, error) {
	pending, err := o.service.GetPendingEmails(ctx)
	if err != nil {
		return nil, err
	}

	var rejected []*data.PendingEmail
	for _, pe := range pending {
		r, err := o.Reject(ctx, pe.ID)
		if err != nil {
			return rejected, fmt.Errorf("failed to reject email %s: %w", pe.ID, err)
		}
		rejected = append(rejected, r)
	}
	return rejected, nil
}

// collectRecipients gathers all recipients from To, CC, BCC fields.
func collectRecipients(to, cc, bcc []string) []string {
	var all []string
	all = append(all, to...)
	all = append(all, cc...)
	all = append(all, bcc...)
	return all
}

// truncatePreview returns the first ~100 characters of text for display.
func truncatePreview(text string) string {
	if len(text) <= 100 {
		return text
	}
	return text[:100] + "..."
}

// queueEmail saves an email to the pending queue and returns a result for the agent.
func (o *EmailOutbox) queueEmail(ctx context.Context, emailType string, recipients []string, subject, body string, requestData any) (*loop.ToolResult, error) {
	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to serialize request: %v", err)), nil
	}

	pe := &data.PendingEmail{
		Type:        emailType,
		Recipients:  strings.Join(recipients, ", "),
		Subject:     subject,
		Preview:     truncatePreview(body),
		RequestData: string(jsonData),
	}

	if err := o.service.AddPendingEmail(ctx, pe); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to queue email: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"status":     "pending_approval",
		"message":    "Email queued for user approval. The user will review it via /outbox.",
		"recipients": pe.Recipients,
		"subject":    subject,
	}), nil
}

// sendAndRecord sends an email immediately and records it in the database with status "approved".
func (o *EmailOutbox) sendAndRecord(ctx context.Context, emailType string, recipients []string, subject, body string, input SendEmailInput) (*loop.ToolResult, error) {
	result, err := o.email.SendEmailTool(ctx, input)
	if err != nil {
		return nil, err
	}

	// Record the sent email in the database
	jsonData, _ := json.Marshal(input)
	pe := &data.PendingEmail{
		Type:        emailType,
		Recipients:  strings.Join(recipients, ", "),
		Subject:     subject,
		Preview:     truncatePreview(body),
		RequestData: string(jsonData),
		Status:      "approved",
	}
	if dbErr := o.service.AddPendingEmail(ctx, pe); dbErr != nil {
		log.Printf("Warning: email sent but failed to record: %v", dbErr)
	}

	return result, nil
}

// --- Tool Method (overrides EmailTools.SendEmailTool) ---

// SendEmailTool intercepts send/reply/forward actions and queues them unless
// all recipients are whitelisted. update_labels passes through directly.
func (o *EmailOutbox) SendEmailTool(ctx context.Context, input SendEmailInput) (*loop.ToolResult, error) {
	action := input.Action
	if action == "" {
		action = "send"
	}

	// update_labels bypasses outbox — no email is sent
	if action == "update_labels" {
		return o.email.SendEmailTool(ctx, input)
	}

	switch action {
	case "send":
		if len(input.To) == 0 {
			return loop.NewErrorResult("at least one recipient (to) is required"), nil
		}
		if input.Subject == "" {
			return loop.NewErrorResult("subject is required"), nil
		}
		if input.Text == "" && input.HTML == "" {
			return loop.NewErrorResult("either text or html body is required"), nil
		}

		allRecipients := collectRecipients(input.To, input.CC, input.BCC)
		body := input.Text
		if body == "" {
			body = input.HTML
		}

		whitelisted, err := o.service.IsEmailWhitelisted(ctx, allRecipients)
		if err == nil && whitelisted {
			return o.sendAndRecord(ctx, "send", allRecipients, input.Subject, body, input)
		}

		return o.queueEmail(ctx, "send", allRecipients, input.Subject, body, input)

	case "reply":
		if input.MessageID == "" {
			return loop.NewErrorResult("message_id is required for reply"), nil
		}
		if input.Text == "" && input.HTML == "" {
			return loop.NewErrorResult("either text or html body is required"), nil
		}

		body := input.Text
		if body == "" {
			body = input.HTML
		}
		recipients := []string{"(reply to message " + input.MessageID + ")"}
		return o.queueEmail(ctx, "reply", recipients, "(reply)", body, input)

	case "forward":
		if input.MessageID == "" {
			return loop.NewErrorResult("message_id is required for forward"), nil
		}
		if len(input.To) == 0 {
			return loop.NewErrorResult("at least one recipient (to) is required"), nil
		}

		allRecipients := collectRecipients(input.To, input.CC, input.BCC)
		whitelisted, err := o.service.IsEmailWhitelisted(ctx, allRecipients)
		if err == nil && whitelisted {
			return o.sendAndRecord(ctx, "forward", allRecipients, "(forward of "+input.MessageID+")", input.Text, input)
		}

		return o.queueEmail(ctx, "forward", allRecipients, "(forward of "+input.MessageID+")", input.Text, input)

	default:
		return loop.NewErrorResult(fmt.Sprintf("invalid action: %q", action)), nil
	}
}

// DescribeTool delegates to EmailTools for tool descriptions.
func (o *EmailOutbox) DescribeTool(name string) string {
	return o.email.DescribeTool(name)
}
