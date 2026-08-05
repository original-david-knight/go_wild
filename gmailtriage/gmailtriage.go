// Package gmailtriage is the Gmail-shaped triage tool group: the schemas an
// agentic_loop agent uses to read a mailbox and sort it — list, get, apply and
// remove a label, archive. It is the Gmail-backed successor to the
// AgentMail-shaped schemas in go_wild/tools/email.go, with two deliberate
// differences.
//
// There is no send. Not a guarded send, not a send behind a flag — the verb
// does not exist in this package, so an agent built on it cannot compose one.
// Reply drafting belongs to whatever owns the mailbox, because a draft is a
// thing a human still has to press a button on and a send is not.
//
// And there is no transport. Every operation goes through the Mailbox seam,
// which the consumer implements — normally with a permission layer over the
// multi-account Gmail clients in go_wild/googleauth. That inversion is the
// point: a policy like "labels only inside one namespace" or "never touch this
// sender" is enforced by the implementation, before any request is issued, and
// this package cannot route around a rule it cannot see.
package gmailtriage

import (
	"context"
	"encoding/json"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// Message is one message as triage sees it: addressing, subject and snippet,
// never a body and never a raw API object. Classification needs the envelope;
// handing an agent the whole message would widen what a compromised prompt can
// exfiltrate for no gain in accuracy.
type Message struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"thread_id"`
	From     string   `json:"from"`
	Subject  string   `json:"subject"`
	Date     string   `json:"date"`
	Snippet  string   `json:"snippet"`
	LabelIDs []string `json:"label_ids"`
}

// Mailbox is the transport seam. Every method takes an account name, so one
// Mailbox serves however many mailboxes the consumer has connected.
//
// Implementations are expected to enforce their own policy and return an error
// rather than perform a refused write. A returned error is reported to the
// agent as a failed tool call, which is what lets a refusal teach the model
// its boundary without the boundary being a matter of prompt wording.
type Mailbox interface {
	ListMessages(ctx context.Context, account, query string, max int64) ([]Message, error)
	GetMessage(ctx context.Context, account, id string) (*Message, error)
	ApplyLabel(ctx context.Context, account, messageID, label string) error
	RemoveLabel(ctx context.Context, account, messageID, label string) error
	Archive(ctx context.Context, account, messageID string) error
}

// Tools returns the triage group as agentic_loop tools, discovered by
// reflection from the method set below.
func Tools(mb Mailbox) []loop.Tool {
	return loop.WrapToolsWithDescriptions(&provider{mb: mb})
}

// ToolNames are the tools this group registers, in registration order. A
// consumer asserting its agent's whole surface reads it from here rather than
// restating the list.
var ToolNames = []string{
	"list_messages", "get_message", "apply_label", "remove_label", "archive_message",
}

type provider struct{ mb Mailbox }

// DescribeTool supplies each tool's description. They state the limit as fact
// rather than as instruction: the boundary is the Mailbox implementation's,
// and a description that pleaded would only be describing it less accurately.
func (p *provider) DescribeTool(name string) string {
	switch name {
	case "list_messages":
		return "List messages in one account's mailbox, newest first (read-only). Returns sender, subject, date and snippet — never the body."
	case "get_message":
		return "Read one message's envelope and snippet by ID (read-only)."
	case "apply_label":
		return "Apply a label to a message. The mailbox restricts which labels may be written; a refused label returns an error and changes nothing."
	case "remove_label":
		return "Remove a label from a message, under the same restriction as apply_label."
	case "archive_message":
		return "Remove a message from the inbox. The mailbox decides whether archiving is permitted at all; refusal returns an error and changes nothing."
	}
	return ""
}

type listMessagesInput struct {
	Account string `json:"account" description:"which mailbox to read"`
	Query   string `json:"query,omitempty" description:"Gmail search query, e.g. 'is:unread newer_than:1d'"`
	Max     int    `json:"max,omitempty" description:"maximum messages to return"`
}

func (p *provider) ListMessagesTool(ctx context.Context, in listMessagesInput) (*loop.ToolResult, error) {
	messages, err := p.mb.ListMessages(ctx, in.Account, in.Query, int64(in.Max))
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return jsonResult(messages)
}

type getMessageInput struct {
	Account string `json:"account" description:"which mailbox the message is in"`
	ID      string `json:"id" description:"the message ID"`
}

func (p *provider) GetMessageTool(ctx context.Context, in getMessageInput) (*loop.ToolResult, error) {
	message, err := p.mb.GetMessage(ctx, in.Account, in.ID)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return jsonResult(message)
}

type labelInput struct {
	Account   string `json:"account" description:"which mailbox the message is in"`
	MessageID string `json:"message_id" description:"the message to label"`
	Label     string `json:"label" description:"the label name"`
}

func (p *provider) ApplyLabelTool(ctx context.Context, in labelInput) (*loop.ToolResult, error) {
	if err := p.mb.ApplyLabel(ctx, in.Account, in.MessageID, in.Label); err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(fmt.Sprintf("applied %s to %s", in.Label, in.MessageID)), nil
}

func (p *provider) RemoveLabelTool(ctx context.Context, in labelInput) (*loop.ToolResult, error) {
	if err := p.mb.RemoveLabel(ctx, in.Account, in.MessageID, in.Label); err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(fmt.Sprintf("removed %s from %s", in.Label, in.MessageID)), nil
}

type archiveInput struct {
	Account   string `json:"account" description:"which mailbox the message is in"`
	MessageID string `json:"message_id" description:"the message to archive"`
}

func (p *provider) ArchiveMessageTool(ctx context.Context, in archiveInput) (*loop.ToolResult, error) {
	if err := p.mb.Archive(ctx, in.Account, in.MessageID); err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult("archived " + in.MessageID), nil
}

func jsonResult(v any) (*loop.ToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return loop.NewSuccessResult(string(raw)), nil
}
