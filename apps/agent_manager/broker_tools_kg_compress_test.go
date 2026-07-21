package main

import (
	"context"
	"strings"
	"testing"
)

func TestCallKnowledgeGraphToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callKnowledgeGraphTools(context.Background(), "kg-agent", "not_a_kg_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallKnowledgeGraphToolsSearchRequiresQuery(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callKnowledgeGraphTools(context.Background(), "kg-agent", "kg_search", []byte(`{}`))
	if !handled {
		t.Fatalf("expected kg_search to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected query validation error")
	}
	if !strings.Contains(err.Error(), "query is required for text search") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallCompressToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callCompressTools(context.Background(), "not_a_compress_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallCompressToolsRequiresContent(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callCompressTools(context.Background(), "compress_content", []byte(`{}`))
	if !handled {
		t.Fatalf("expected compress_content to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected content validation error")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsKnowledgeGraphToolRecognition(t *testing.T) {
	if !isKnowledgeGraphTool("kg_search") {
		t.Fatalf("expected kg_search to be recognized")
	}
	if isKnowledgeGraphTool("kg_not_real") {
		t.Fatalf("expected unknown kg tool to be rejected")
	}
}

func TestIsCompressToolRecognition(t *testing.T) {
	if !isCompressTool("compress_content") {
		t.Fatalf("expected compress_content to be recognized")
	}
	if isCompressTool("compress_not_real") {
		t.Fatalf("expected unknown compress tool to be rejected")
	}
}
