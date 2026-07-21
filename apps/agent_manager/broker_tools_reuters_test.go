package main

import (
	"testing"
)

func TestIsReutersTool(t *testing.T) {
	expected := []string{"reuters_news", "search_reuters_news", "read_reuters_article"}
	for _, name := range expected {
		if !isReutersTool(name) {
			t.Errorf("isReutersTool(%q) = false, want true", name)
		}
	}

	if isReutersTool("not_a_tool") {
		t.Error("isReutersTool(not_a_tool) = true, want false")
	}
}

func TestCallReutersTools_Unknown(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	handled, _, _ := h.callReutersTools(nil, "not_reuters", nil)
	if handled {
		t.Error("expected unknown tool to not be handled")
	}
}
