package main

import (
	"context"
	"fmt"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

type cacheToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error)

type cacheGetInput struct {
	Key string `json:"key"`
}

type cacheSetInput struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	TTLSecs int    `json:"ttl_secs"`
}

var cacheToolHandlers = map[string]cacheToolHandlerFunc{
	"cache_get": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		return callWithInput[cacheGetInput](inputJSON, func(input cacheGetInput) (any, error) {
			if input.Key == "" {
				return nil, fmt.Errorf("key is required")
			}
			cache := gowild_data.NewCache(h.db)
			val, ok := cache.Get(ctx, input.Key)
			return map[string]any{"value": val, "found": ok}, nil
		})
	},
	"cache_set": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		return callWithInput[cacheSetInput](inputJSON, func(input cacheSetInput) (any, error) {
			if input.Key == "" {
				return nil, fmt.Errorf("key is required")
			}
			if input.TTLSecs <= 0 {
				return nil, fmt.Errorf("ttl_secs must be positive")
			}
			cache := gowild_data.NewCache(h.db)
			if err := cache.Set(ctx, input.Key, input.Value, time.Duration(input.TTLSecs)*time.Second); err != nil {
				return nil, err
			}
			return map[string]any{"stored": true}, nil
		})
	},
}

func isCacheTool(toolName string) bool {
	_, ok := cacheToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callCacheTools(ctx context.Context, toolName string, inputJSON []byte) (bool, any, error) {
	if !isCacheTool(toolName) {
		return false, nil, nil
	}

	handler, ok := cacheToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, inputJSON)
	return true, result, err
}
