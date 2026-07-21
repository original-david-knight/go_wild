package main

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
)

type webReaderToolHandlerFunc func(ctx context.Context, wt *tools.WebReaderTools, inputJSON []byte) (any, error)

var webReaderToolHandlers = map[string]webReaderToolHandlerFunc{
	"read_webpage": func(ctx context.Context, wt *tools.WebReaderTools, inputJSON []byte) (any, error) {
		return callWithInput[tools.ReadWebpageInput](inputJSON, func(input tools.ReadWebpageInput) (any, error) {
			return toolResultContent(wt.ReadWebpageTool(ctx, input))
		})
	},
}

func isWebReaderTool(toolName string) bool {
	_, ok := webReaderToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callWebReaderTools(ctx context.Context, toolName string, inputJSON []byte) (bool, any, error) {
	handler, ok := webReaderToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}

	wt := tools.NewWebReaderTools(nil)
	result, err := handler(ctx, wt, inputJSON)
	return true, result, err
}
