package main

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type reportToolHandlerFunc func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

var reportToolHandlers = map[string]reportToolHandlerFunc{
	"set_report_html": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		report := tools.NewReportTools(svc)
		return callWithInput[tools.SetReportHTMLInput](inputJSON, func(input tools.SetReportHTMLInput) (any, error) {
			r, err := report.SetReportHTMLTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"get_report_html": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		report := tools.NewReportTools(svc)
		return callWithInput[tools.GetReportHTMLInput](inputJSON, func(input tools.GetReportHTMLInput) (any, error) {
			r, err := report.GetReportHTMLTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func isReportTool(toolName string) bool {
	_, ok := reportToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callReportTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isReportTool(toolName) {
		return false, nil, nil
	}

	handler, ok := reportToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(ctx, svc, inputJSON)
	return true, result, err
}
