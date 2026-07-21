package deepresearch

import "fmt"

var buildSearcher = func() (Searcher, error) {
	return NewGeminiGroundedSearcher()
}

var buildFetcher = func() (Fetcher, error) {
	return NewWebpageFetcher()
}

func NewSearcher() (Searcher, string, error) {
	searcher, err := buildSearcher()
	if err != nil {
		return nil, "", fmt.Errorf("deep research searcher unavailable: gemini_grounded_search=%s", err)
	}
	return searcher, "gemini_grounded_search", nil
}

func NewFetcher() (Fetcher, string, error) {
	fetcher, err := buildFetcher()
	if err != nil {
		return nil, "", err
	}
	return fetcher, "read_webpage_tool_fetcher", nil
}
