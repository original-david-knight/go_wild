package deepresearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepResearchWebpageFetcherFetchesFullPageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Research Page</title></head><body><h1>Main</h1><p>This is a full page content body.</p><p>Second paragraph.</p></body></html>`))
	}))
	defer server.Close()

	fetcher, err := NewWebpageFetcher()
	if err != nil {
		t.Fatalf("NewWebpageFetcher failed: %v", err)
	}
	doc, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if strings.TrimSpace(doc.URL) != server.URL {
		t.Fatalf("unexpected document URL: %q", doc.URL)
	}
	if strings.TrimSpace(doc.Title) != "Research Page" {
		t.Fatalf("unexpected title: %q", doc.Title)
	}
	if !strings.Contains(doc.Content, "full page content body") {
		t.Fatalf("expected full page body text in fetched content, got: %s", doc.Content)
	}
	if !strings.Contains(doc.Content, "Second paragraph") {
		t.Fatalf("expected second paragraph text in fetched content, got: %s", doc.Content)
	}
}

func TestDeepResearchWebpageFetcherRejectsUnsupportedScheme(t *testing.T) {
	fetcher, err := NewWebpageFetcher()
	if err != nil {
		t.Fatalf("NewWebpageFetcher failed: %v", err)
	}
	_, err = fetcher.Fetch(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatalf("expected unsupported scheme error")
	}
}

func TestDeepResearchWebpageFetcherReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	fetcher, err := NewWebpageFetcher()
	if err != nil {
		t.Fatalf("NewWebpageFetcher failed: %v", err)
	}
	_, err = fetcher.Fetch(context.Background(), server.URL+"/missing")
	if err == nil {
		t.Fatalf("expected error for 404")
	}

	var httpErr *fetchHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTP error, got %T: %v", err, err)
	}
	if httpErr.StatusCode != 404 {
		t.Fatalf("expected status 404, got %d", httpErr.StatusCode)
	}
}
