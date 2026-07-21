package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

type ContextDump struct {
	GeneratedAt       string               `json:"generated_at"`
	MessageCount      int                  `json:"message_count"`
	ApproxTokens      int                  `json:"approx_tokens"`
	ToolOutputsMasked int                  `json:"tool_outputs_masked"`
	ToolOutputsFull   int                  `json:"tool_outputs_full"`
	Messages          []ContextDumpMessage `json:"messages"`
}

type ContextDumpMessage struct {
	Index int               `json:"index"`
	Role  string            `json:"role"`
	Parts []ContextDumpPart `json:"parts"`
}

type ContextDumpPart struct {
	Type     string         `json:"type"` // text, tool_call, tool_result, inline_data
	Text     string         `json:"text,omitempty"`
	ToolName string         `json:"tool_name,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
	Response map[string]any `json:"response,omitempty"`
	Masked   bool           `json:"masked,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	MimeType string         `json:"mime_type,omitempty"`
	Bytes    int            `json:"bytes,omitempty"`
}

func handleContextDumpCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	if ctx.history == nil {
		output.Error("context dump unavailable (no history)")
		return cmdContinue
	}

	dump := buildContextDump(*ctx.history)
	payload, err := json.Marshal(dump)
	if err != nil {
		output.Error("context dump failed: %v", err)
		return cmdContinue
	}

	output.ContextDump(string(payload))
	return cmdContinue
}

func buildContextDump(history []loop.Message) ContextDump {
	dump := ContextDump{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		MessageCount: len(history),
		ApproxTokens: estimateHistorySize(history) / 4,
	}

	for i, msg := range history {
		entry := ContextDumpMessage{
			Index: i,
			Role:  string(msg.Role),
		}

		if msg.Content != nil {
			entry.Parts = append(entry.Parts, extractContextParts(msg.Content.Parts, &dump)...)
		}

		dump.Messages = append(dump.Messages, entry)
	}

	return dump
}

func extractContextParts(parts []*genai.Part, dump *ContextDump) []ContextDumpPart {
	out := make([]ContextDumpPart, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != "":
			out = append(out, ContextDumpPart{
				Type: "text",
				Text: part.Text,
			})
		case part.FunctionCall != nil:
			out = append(out, ContextDumpPart{
				Type:     "tool_call",
				ToolName: part.FunctionCall.Name,
				Args:     part.FunctionCall.Args,
			})
		case part.FunctionResponse != nil:
			masked, summary := detectMaskedSummary(part.FunctionResponse.Response)
			if masked {
				dump.ToolOutputsMasked++
			} else {
				dump.ToolOutputsFull++
			}
			out = append(out, ContextDumpPart{
				Type:     "tool_result",
				ToolName: part.FunctionResponse.Name,
				Response: part.FunctionResponse.Response,
				Masked:   masked,
				Summary:  summary,
			})
		case part.InlineData != nil:
			out = append(out, ContextDumpPart{
				Type:     "inline_data",
				MimeType: part.InlineData.MIMEType,
				Bytes:    len(part.InlineData.Data),
			})
		}
	}
	return out
}

func detectMaskedSummary(resp map[string]any) (bool, string) {
	if resp == nil {
		return false, ""
	}
	if v, ok := resp["_masked"]; ok {
		return true, fmt.Sprint(v)
	}
	return false, ""
}
