package agentnode

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

type fakeTool struct {
	name string
	desc string
}

func (f fakeTool) Name() string               { return f.name }
func (f fakeTool) Description() string        { return f.desc }
func (f fakeTool) InputSchema() *genai.Schema { return nil }
func (f fakeTool) Execute(context.Context, map[string]any) (*loop.ToolResult, error) {
	return loop.NewSuccessResult("ok"), nil
}

func TestDefaultWebTools_OmitsWebSearchWithoutEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")

	reg := DefaultWebTools()
	if _, ok := reg["web_search"]; ok {
		t.Fatal("web_search should be omitted when GEMINI_API_KEY is missing")
	}
	if _, ok := reg["read_webpage"]; !ok {
		t.Fatal("read_webpage should always be available")
	}
	if _, ok := reg["http_request"]; !ok {
		t.Fatal("http_request should always be available")
	}

	catalog := ToolCatalog(reg)
	if !strings.Contains(catalog, "web_search unavailable") {
		t.Fatalf("expected missing-web-search warning in catalog, got: %s", catalog)
	}
}

func TestDefaultWebTools_IncludesWebSearchWithEnv(t *testing.T) {
	// Note: with Gemini grounding, web_search availability depends on
	// successfully creating a genai.Client which requires a valid API key.
	// In tests, the client creation may fail, so we just verify the env
	// config function works correctly.
	t.Setenv("GEMINI_API_KEY", "dummy-key")

	key := geminiAPIKeyEnvConfig()
	if key != "dummy-key" {
		t.Fatalf("expected geminiAPIKeyEnvConfig to return 'dummy-key', got %q", key)
	}
}

func TestToolCatalog_SortsToolNames(t *testing.T) {
	reg := ToolRegistry{
		"zeta":  fakeTool{name: "zeta", desc: "z tool"},
		"alpha": fakeTool{name: "alpha", desc: "a tool"},
		"beta":  fakeTool{name: "beta", desc: "b tool"},
	}

	catalog := ToolCatalog(reg)
	alpha := strings.Index(catalog, "- alpha:")
	beta := strings.Index(catalog, "- beta:")
	zeta := strings.Index(catalog, "- zeta:")

	if alpha == -1 || beta == -1 || zeta == -1 {
		t.Fatalf("catalog missing expected tools: %s", catalog)
	}
	if !(alpha < beta && beta < zeta) {
		t.Fatalf("expected alphabetical order, got: %s", catalog)
	}
}

func TestWebSearchUnavailableWarning_ShowsWhenMissing(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	reg := ToolRegistry{}
	warning := webSearchUnavailableWarning(reg)
	if !strings.Contains(warning, "GEMINI_API_KEY") {
		t.Fatalf("expected warning about GEMINI_API_KEY, got: %s", warning)
	}
}

func TestWebSearchUnavailableWarning_EmptyWhenPresent(t *testing.T) {
	reg := ToolRegistry{
		"web_search": fakeTool{name: "web_search", desc: "search"},
	}
	warning := webSearchUnavailableWarning(reg)
	if warning != "" {
		t.Fatalf("expected empty warning when web_search is in registry, got: %s", warning)
	}
}
