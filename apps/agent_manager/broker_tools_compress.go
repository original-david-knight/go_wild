package main

import (
	"context"
	"fmt"
	"os"

	"github.com/original-david-knight/go_wild/my"
	"google.golang.org/genai"
)

type compressContentInput struct {
	Content string `json:"content"`
}

const compressContentPrompt = `You are a content filter. You will receive markdown scraped from a webpage.
Your job is to remove ONLY site chrome and UI elements. Keep ALL article/page content intact.

Remove ONLY these UI elements:
- Site navigation menus, header/footer links, breadcrumbs
- Cookie/privacy banners
- Ads and promotional banners
- Social media share buttons
- Newsletter signup forms
- Login/signup prompts
- Links to other language versions of the page

KEEP all of the following — these are content, not chrome:
- The full article or page body
- Tables of contents
- References, citations, footnotes
- "See also" and related topic links within the article
- Categories and tags
- Code examples, tables, lists
- Author info and publication dates

Output the filtered content preserving original markdown formatting exactly.
Do not summarize or rewrite — just strip the site UI wrapper.`

type compressToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error)

var compressToolHandlers = map[string]compressToolHandlerFunc{
	"compress_content": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		return callWithInput[compressContentInput](inputJSON, func(input compressContentInput) (any, error) {
			if input.Content == "" {
				return nil, fmt.Errorf("content is required")
			}
			if h.genaiClient == nil {
				return nil, fmt.Errorf("genai client not initialized")
			}
			model := gowild_my.GetEnvOrDefault("FAST_MODEL", "gemini-3-flash-preview")
			// DEBUG: log before/after to /tmp/compress_debug.log
			debugLog, _ := os.OpenFile("/tmp/compress_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if debugLog != nil {
				fmt.Fprintf(debugLog, "\n===== BEFORE (%d bytes) =====\n%s\n", len(input.Content), input.Content)
			}
			resp, err := h.genaiClient.Models.GenerateContent(ctx, model, genai.Text(input.Content), &genai.GenerateContentConfig{
				SystemInstruction: genai.NewContentFromText(compressContentPrompt, genai.RoleUser),
			})
			if err != nil {
				if debugLog != nil {
					fmt.Fprintf(debugLog, "===== ERROR =====\n%v\n", err)
					debugLog.Close()
				}
				return nil, fmt.Errorf("gemini: %w", err)
			}
			compressed := resp.Text()
			if debugLog != nil {
				fmt.Fprintf(debugLog, "===== AFTER (%d bytes, %.0f%% reduction) =====\n%s\n", len(compressed), (1-float64(len(compressed))/float64(len(input.Content)))*100, compressed)
				debugLog.Close()
			}
			return map[string]any{"content": compressed}, nil
		})
	},
}

func isCompressTool(toolName string) bool {
	_, ok := compressToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callCompressTools(ctx context.Context, toolName string, inputJSON []byte) (bool, any, error) {
	if !isCompressTool(toolName) {
		return false, nil, nil
	}

	handler, ok := compressToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, inputJSON)
	return true, result, err
}
