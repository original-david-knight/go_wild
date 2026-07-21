package main

import (
	"fmt"
	"strings"

	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

var buildGeminiDeepResearchSearcher = func() (deepresearch.Searcher, error) {
	return deepresearch.NewGeminiGroundedSearcher()
}

var buildClaudeDeepResearchSearcher = func() (deepresearch.Searcher, error) {
	return deepresearch.NewClaudeSearcher(), nil
}

var buildCodexDeepResearchSearcher = func() (deepresearch.Searcher, error) {
	return deepresearch.DefaultCodexSearcher()
}

var buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) {
	return deepresearch.NewWebpageFetcher()
}

func newDeepResearchSearcher(llmBackend string) (deepresearch.Searcher, string, error) {
	switch strings.ToLower(strings.TrimSpace(llmBackend)) {
	case "", "gemini":
		searcher, err := buildGeminiDeepResearchSearcher()
		if err != nil {
			return nil, "", fmt.Errorf("deep research searcher unavailable: gemini_grounded_search=%s", err)
		}
		return searcher, "gemini_grounded_search", nil
	case "claude":
		searcher, err := buildClaudeDeepResearchSearcher()
		if err != nil {
			return nil, "", fmt.Errorf("deep research searcher unavailable: claude_web_search=%s", err)
		}
		return searcher, "claude_web_search", nil
	case "codex":
		searcher, err := buildCodexDeepResearchSearcher()
		if err != nil {
			return nil, "", fmt.Errorf("deep research searcher unavailable: codex_web_search=%s", err)
		}
		return searcher, "codex_web_search", nil
	default:
		return nil, "", fmt.Errorf("deep research searcher unavailable: llm_backend %q is not supported", llmBackend)
	}
}

func newDeepResearchFetcher() (deepresearch.Fetcher, string, error) {
	fetcher, err := buildDeepResearchFetcher()
	if err != nil {
		return nil, "", err
	}
	return fetcher, "read_webpage_tool_fetcher", nil
}
