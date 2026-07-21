package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SetReportHTMLInput defines the input for setting report HTML.
type SetReportHTMLInput struct {
	HTML string `json:"html" description:"HTML content to render in the manager report tab. Provide a full document or body fragment." required:"true"`
}

// GetReportHTMLInput defines input for reading report HTML.
type GetReportHTMLInput struct{}

// ReportTools provides tools for managing an agent's report HTML.
type ReportTools struct {
	service *data.AgentService
}

// NewReportTools creates a new ReportTools instance.
func NewReportTools(service *data.AgentService) *ReportTools {
	return &ReportTools{service: service}
}

// SetReportHTMLTool updates the agent's report HTML.
func (t *ReportTools) SetReportHTMLTool(ctx context.Context, input SetReportHTMLInput) (*loop.ToolResult, error) {
	const maxLength = 200_000
	if len(input.HTML) > maxLength {
		return loop.NewErrorResult(fmt.Sprintf("report HTML too long: %d chars (max %d)", len(input.HTML), maxLength)), nil
	}

	updatedAt, err := t.service.SetReportHTML(ctx, input.HTML)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to save report HTML: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success":    true,
		"length":     len(input.HTML),
		"updated_at": updatedAt.Format(time.RFC3339),
		"message":    "Report HTML updated. It will appear in the manager UI Report tab.",
	}), nil
}

// GetReportHTMLTool reads the agent's report HTML.
func (t *ReportTools) GetReportHTMLTool(ctx context.Context, _ GetReportHTMLInput) (*loop.ToolResult, error) {
	html, updatedAt, err := t.service.GetReportHTML(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to read report HTML: %v", err)), nil
	}

	result := map[string]any{
		"exists": html != "",
		"html":   html,
	}
	if !updatedAt.IsZero() {
		result["updated_at"] = updatedAt.Format(time.RFC3339)
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (t *ReportTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"set_report_html": "Set HTML content for the manager UI Report tab. Use this for rich, formatted reports (tables, charts, summaries).",
		"get_report_html": "Get the current report HTML and its last update time.",
	}
	return descriptions[name]
}
