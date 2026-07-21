package main

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type soulToolHandlerFunc func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

var soulToolHandlers = map[string]soulToolHandlerFunc{
	"read_soul": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		soul := tools.NewSoulTools(svc)
		return callWithInput[tools.ReadSoulInput](inputJSON, func(input tools.ReadSoulInput) (any, error) {
			r, err := soul.ReadSoulTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"update_soul": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		soul := tools.NewSoulTools(svc)
		return callWithInput[tools.UpdateSoulInput](inputJSON, func(input tools.UpdateSoulInput) (any, error) {
			r, err := soul.UpdateSoulTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func isSoulTool(toolName string) bool {
	_, ok := soulToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callSoulTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isSoulTool(toolName) {
		return false, nil, nil
	}

	handler, ok := soulToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(ctx, svc, inputJSON)
	return true, result, err
}
