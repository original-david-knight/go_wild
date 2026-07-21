package agentnode

import (
	"os"
	"sort"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	tools "github.com/original-david-knight/go_wild/tools"
)

// DefaultWebTools returns a ToolRegistry with the standard web tools:
// web_search, read_webpage, http_request, and Reuters news tools.
// Web search requires GEMINI_API_KEY (uses Gemini Grounding with Google Search).
// If it is missing, web_search is omitted.
func DefaultWebTools() ToolRegistry {
	registry := ToolRegistry{}

	// Web search (requires Gemini API key for Grounding with Google Search)
	searchKey := geminiAPIKeyEnvConfig()
	if searchKey != "" {
		webTools := tools.NewWebTools(searchKey)
		if webTools.Available() {
			for _, t := range loop.WrapToolsWithDescriptions(webTools) {
				registry[t.Name()] = t
			}
		}
	}

	// Web page reader (no compression — standalone, no broker)
	webReader := tools.NewWebReaderTools(nil)
	for _, t := range loop.WrapToolsWithDescriptions(webReader) {
		registry[t.Name()] = t
	}

	// HTTP request tool
	httpTools := tools.NewHTTPTools()
	for _, t := range loop.WrapToolsWithDescriptions(httpTools) {
		registry[t.Name()] = t
	}

	// Reuters news tools (search, read articles)
	reutersTools := tools.NewReutersTools(nil)
	for _, t := range loop.WrapToolsWithDescriptions(reutersTools) {
		registry[t.Name()] = t
	}

	return registry
}

// ToolCatalog returns a human-readable description of available tools,
// suitable for including in a planner prompt.
func ToolCatalog(registry ToolRegistry) string {
	warning := webSearchUnavailableWarning(registry)
	if len(registry) == 0 {
		msg := "No tools available. All nodes must be single-shot (no tools)."
		if warning == "" {
			return msg
		}
		return msg + "\n" + warning
	}

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("Available tools that can be assigned to nodes:\n")
	for _, name := range names {
		t := registry[name]
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteString(": ")
		desc := t.Description()
		// First sentence only
		if idx := strings.Index(desc, "\n"); idx > 0 {
			desc = desc[:idx]
		}
		if len(desc) > 150 {
			desc = desc[:147] + "..."
		}
		sb.WriteString(desc)
		sb.WriteString("\n")
	}
	if warning != "" {
		sb.WriteString("- ")
		sb.WriteString(warning)
		sb.WriteString("\n")
	}
	return sb.String()
}

func geminiAPIKeyEnvConfig() string {
	return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
}

func webSearchUnavailableWarning(registry ToolRegistry) string {
	if _, ok := registry["web_search"]; ok {
		return ""
	}

	apiKey := geminiAPIKeyEnvConfig()
	if apiKey != "" {
		return ""
	}
	return "web_search unavailable: set GEMINI_API_KEY"
}
