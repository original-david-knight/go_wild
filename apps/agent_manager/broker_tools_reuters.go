package main

import (
	"context"

	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/tools"
)

type reutersToolHandlerFunc func(ctx context.Context, rt *tools.ReutersTools, inputJSON []byte) (any, error)

var reutersToolHandlers = map[string]reutersToolHandlerFunc{
	"reuters_news": func(ctx context.Context, rt *tools.ReutersTools, inputJSON []byte) (any, error) {
		return callWithInput[tools.ReutersNewsInput](inputJSON, func(input tools.ReutersNewsInput) (any, error) {
			return toolResultContent(rt.ReutersNewsTool(ctx, input))
		})
	},
	"search_reuters_news": func(ctx context.Context, rt *tools.ReutersTools, inputJSON []byte) (any, error) {
		return callWithInput[tools.SearchReutersNewsInput](inputJSON, func(input tools.SearchReutersNewsInput) (any, error) {
			return toolResultContent(rt.SearchReutersNewsTool(ctx, input))
		})
	},
	"read_reuters_article": func(ctx context.Context, rt *tools.ReutersTools, inputJSON []byte) (any, error) {
		return callWithInput[tools.ReadReutersArticleInput](inputJSON, func(input tools.ReadReutersArticleInput) (any, error) {
			return toolResultContent(rt.ReadReutersArticleTool(ctx, input))
		})
	},
}

func isReutersTool(toolName string) bool {
	_, ok := reutersToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callReutersTools(ctx context.Context, toolName string, inputJSON []byte) (bool, any, error) {
	handler, ok := reutersToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}

	cache := gowild_data.NewCache(h.db)
	rt := tools.NewReutersTools(cache)
	result, err := handler(ctx, rt, inputJSON)
	return true, result, err
}
