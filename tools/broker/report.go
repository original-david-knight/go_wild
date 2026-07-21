package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ReportTools proxies report HTML operations through the broker API.
type ReportTools struct {
	client *Client
}

func NewReportTools(client *Client) *ReportTools {
	return &ReportTools{client: client}
}

func (r *ReportTools) SetReportHTMLTool(ctx context.Context, input tools.SetReportHTMLInput) (*loop.ToolResult, error) {
	result, err := r.client.CallTool(ctx, "set_report_html", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (r *ReportTools) GetReportHTMLTool(ctx context.Context, input tools.GetReportHTMLInput) (*loop.ToolResult, error) {
	result, err := r.client.CallTool(ctx, "get_report_html", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (r *ReportTools) DescribeTool(name string) string {
	return tools.NewReportTools(nil).DescribeTool(name)
}
