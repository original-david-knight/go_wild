package main

import (
	"context"
	"sort"
	"strings"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

type deepResearchToolSpec struct {
	ToolName       string
	Method         string
	Description    string
	QueryTemplate  string
	InputSchema    any
	ResearchSchema any
	Options        any
}

func (s deepResearchToolSpec) asMap() map[string]any {
	item := map[string]any{
		"tool_name":       s.ToolName,
		"method":          s.Method,
		"description":     s.Description,
		"query_template":  s.QueryTemplate,
		"provider":        "deep_research",
		"provider_source": "global_deep_research_methods",
	}
	if s.InputSchema != nil {
		item["input_schema"] = s.InputSchema
	}
	if s.ResearchSchema != nil {
		item["research_schema"] = s.ResearchSchema
	}
	if s.Options != nil {
		item["options"] = s.Options
	}
	return item
}

func listDeepResearchMethodTools(ctx context.Context, db gowild_data.Database) ([]deepResearchToolSpec, error) {
	methods, err := NewAgentService(db).ListDeepResearchMethods(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]deepResearchToolSpec, 0, len(methods))
	for _, m := range methods {
		method := strings.TrimSpace(m.Method)
		if method == "" || !m.Enabled {
			continue
		}

		spec := deepResearchToolSpec{
			ToolName:      method,
			Method:        method,
			Description:   strings.TrimSpace(m.Description),
			QueryTemplate: strings.TrimSpace(m.QueryTemplate),
		}
		if parsed, err := parseCapabilitySchema(m.InputSchemaJSON); err == nil && parsed != nil {
			spec.InputSchema = parsed
		}
		if parsed, err := parseCapabilitySchema(m.ResearchSchemaJSON); err == nil && parsed != nil {
			spec.ResearchSchema = parsed
		}
		if parsed, err := parseCapabilitySchema(m.OptionsJSON); err == nil && parsed != nil {
			spec.Options = parsed
		}
		out = append(out, spec)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Method < out[j].Method
	})
	return out, nil
}

func deepResearchToolSpecForName(ctx context.Context, db gowild_data.Database, toolName string) (deepResearchToolSpec, bool, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return deepResearchToolSpec{}, false, nil
	}
	specs, err := listDeepResearchMethodTools(ctx, db)
	if err != nil {
		return deepResearchToolSpec{}, false, err
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.ToolName) == toolName {
			return spec, true, nil
		}
	}
	return deepResearchToolSpec{}, false, nil
}
