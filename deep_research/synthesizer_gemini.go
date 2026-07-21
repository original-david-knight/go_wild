package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/genai"
)

type geminiDeepResearchSynthesizer struct {
	client          *genai.Client
	model           string
	generateContent func(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

func NewGeminiSynthesizer() (*geminiDeepResearchSynthesizer, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API not configured (set GEMINI_API_KEY)")
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini synthesizer client: %w", err)
	}

	model := deepResearchModelFromEnv("DEEP_RESEARCH_SYNTHESIZER_MODEL", "SMART_MODEL")
	return &geminiDeepResearchSynthesizer{
		client: client,
		model:  model,
		generateContent: retryOnRateLimit(func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return client.Models.GenerateContent(ctx, model, contents, cfg)
		}),
	}, nil
}

func (s *geminiDeepResearchSynthesizer) Synthesize(ctx context.Context, req SynthesisRequest) (SynthesisResult, error) {
	if len(req.Schema) == 0 {
		return SynthesisResult{}, nil
	}
	if s == nil || s.generateContent == nil {
		return SynthesisResult{}, fmt.Errorf("synthesizer model client is not configured")
	}

	prompt := deepResearchSynthesisPrompt(req)
	temp := float32(0.1)
	cfg := &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  16384,
		ResponseMIMEType: "application/json",
		ResponseSchema:   deepResearchSynthesisResponseSchema(req.Schema),
	}

	resp, err := s.generateContent(ctx, s.model, genai.Text(prompt), cfg)
	if err != nil {
		// Fall back to non-schema JSON if the selected model rejects structured output.
		resp, err = s.generateContent(ctx, s.model, genai.Text(prompt), &genai.GenerateContentConfig{
			Temperature:      &temp,
			MaxOutputTokens:  16384,
			ResponseMIMEType: "application/json",
		})
		if err != nil {
			return SynthesisResult{}, err
		}
	}

	raw := strings.TrimSpace(deepResearchCandidateText(firstCandidateContent(resp)))
	if raw == "" {
		return SynthesisResult{}, fmt.Errorf("synthesizer returned empty response")
	}

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		if outputRaw, ok := wrapped["output"]; ok {
			var output any
			if err := json.Unmarshal(outputRaw, &output); err != nil {
				return SynthesisResult{}, fmt.Errorf("synthesizer output field is invalid JSON: %w", err)
			}
			result := SynthesisResult{Output: output}
			if summaryRaw, ok := wrapped["summary"]; ok {
				var summary string
				if err := json.Unmarshal(summaryRaw, &summary); err == nil {
					result.Summary = strings.TrimSpace(summary)
				}
			}
			return result, nil
		}
	}

	var output any
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return SynthesisResult{}, fmt.Errorf("synthesizer returned invalid JSON: %w", err)
	}
	return SynthesisResult{Output: output}, nil
}

func deepResearchSynthesisResponseSchema(schema map[string]any) *genai.Schema {
	outputSchema := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if outputSchema == nil {
		outputSchema = &genai.Schema{Type: genai.TypeObject}
	}

	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"output":  outputSchema,
			"summary": {Type: genai.TypeString},
		},
		Required: []string{"output"},
	}
}

func deepResearchSynthesisPrompt(req SynthesisRequest) string {
	problem := strings.TrimSpace(req.Guidance)
	if problem == "" {
		problem = strings.TrimSpace(req.Query)
	}
	excludedDomains := strings.TrimSpace(strings.Join(req.ExcludedDomains, ", "))
	if excludedDomains == "" {
		excludedDomains = "none"
	}

	objectiveStatus := make([]string, 0, len(req.ObjectiveResults))
	for _, item := range req.ObjectiveResults {
		key := strings.TrimSpace(item.Objective.Key)
		if key == "" {
			continue
		}
		line := fmt.Sprintf("%s: %s (evidence_count=%d)", key, item.Status, item.EvidenceCount)
		if item.BestFinding != nil && strings.TrimSpace(item.BestFinding.URL) != "" {
			line += " source=" + strings.TrimSpace(item.BestFinding.URL)
		}
		objectiveStatus = append(objectiveStatus, line)
	}
	sort.Strings(objectiveStatus)

	sourceLines := make([]string, 0, len(req.Sources))
	for i, source := range req.Sources {
		if i >= 10 {
			break
		}
		u := strings.TrimSpace(source.URL)
		if u == "" {
			continue
		}
		line := u
		if title := strings.TrimSpace(source.Title); title != "" {
			line = title + " | " + u
		}
		sourceLines = append(sourceLines, line)
	}

	schemaText := "{}"
	if len(req.Schema) > 0 {
		if blob, err := json.Marshal(req.Schema); err == nil {
			schemaText = string(blob)
		}
	}

	findingsText := "{}"
	if len(req.Findings) > 0 {
		type compactFinding struct {
			URL     string  `json:"url"`
			Title   string  `json:"title,omitempty"`
			Snippet string  `json:"snippet,omitempty"`
			Excerpt string  `json:"excerpt,omitempty"`
			Score   float64 `json:"score"`
		}
		compact := map[string][]compactFinding{}
		for key, rows := range req.Findings {
			if len(rows) == 0 {
				continue
			}
			items := make([]compactFinding, 0, len(rows))
			for i, row := range rows {
				if i >= 3 {
					break
				}
				items = append(items, compactFinding{
					URL:     row.URL,
					Title:   row.Title,
					Snippet: row.Snippet,
					Excerpt: row.Excerpt,
					Score:   row.Score,
				})
			}
			compact[key] = items
		}
		if blob, err := json.Marshal(compact); err == nil {
			findingsText = string(blob)
		}
	}

	warningsText := strings.TrimSpace(strings.Join(req.Warnings, "\n"))
	if warningsText == "" {
		warningsText = "none"
	}

	now := deepResearchCurrentTime()

	return fmt.Sprintf(`You are working on task within the following problem:
%s

Current date and time: %s

You are a schema-grounded synthesis engine for deep research.
Use only the provided evidence to populate the target JSON schema.
Do not hallucinate missing facts. If evidence is insufficient for a field, set that field to null.
Search snippets are discovery hints only; rely on fetched page excerpts for factual claims.
When the topic is affected by current events, recent news, or market movements, prioritize the most recent evidence and note any time-sensitive factors in the summary. Outdated information should be flagged or deprioritized.
Never use excluded domains as evidence. Excluded domains: %s
Return strict JSON with fields:
- output: value matching the target schema
- summary: concise summary of evidence quality and remaining uncertainty

Base query:
%s

Rounds executed: %d

Objective status:
%s

Top sources:
%s

Current findings (compact JSON):
%s

Warnings:
%s

Target schema JSON:
%s`,
		problem,
		now,
		excludedDomains,
		strings.TrimSpace(req.Query),
		req.Rounds,
		strings.Join(objectiveStatus, "\n"),
		strings.Join(sourceLines, "\n"),
		findingsText,
		warningsText,
		schemaText,
	)
}

func deepResearchJSONSchemaToGenAISchema(raw any, depth int) *genai.Schema {
	if depth > 12 {
		return &genai.Schema{Type: genai.TypeObject}
	}
	node, ok := raw.(map[string]any)
	if !ok || len(node) == 0 {
		return &genai.Schema{Type: genai.TypeObject}
	}

	normalized, nullable := deepResearchNormalizeJSONSchemaNode(node)
	schema := &genai.Schema{}
	if desc := strings.TrimSpace(deepResearchSchemaString(normalized["description"])); desc != "" {
		schema.Description = desc
	}
	if format := strings.TrimSpace(deepResearchSchemaString(normalized["format"])); format != "" {
		schema.Format = format
	}
	if pattern := strings.TrimSpace(deepResearchSchemaString(normalized["pattern"])); pattern != "" {
		schema.Pattern = pattern
	}
	if nullable {
		b := true
		schema.Nullable = &b
	}

	typeName := deepResearchJSONSchemaTypeName(normalized)
	switch typeName {
	case "array":
		schema.Type = genai.TypeArray
		schema.Items = deepResearchJSONSchemaToGenAISchema(normalized["items"], depth+1)
		if schema.Items == nil {
			schema.Items = &genai.Schema{Type: genai.TypeString}
		}
		if v, ok := deepResearchSchemaInt64(normalized["minItems"]); ok {
			schema.MinItems = &v
		}
		if v, ok := deepResearchSchemaInt64(normalized["maxItems"]); ok {
			schema.MaxItems = &v
		}
	case "string":
		schema.Type = genai.TypeString
		if v, ok := deepResearchSchemaInt64(normalized["minLength"]); ok {
			schema.MinLength = &v
		}
		if v, ok := deepResearchSchemaInt64(normalized["maxLength"]); ok {
			schema.MaxLength = &v
		}
	case "integer":
		schema.Type = genai.TypeInteger
		if v, ok := deepResearchSchemaFloat64(normalized["minimum"]); ok {
			schema.Minimum = &v
		}
		if v, ok := deepResearchSchemaFloat64(normalized["maximum"]); ok {
			schema.Maximum = &v
		}
	case "number":
		schema.Type = genai.TypeNumber
		if v, ok := deepResearchSchemaFloat64(normalized["minimum"]); ok {
			schema.Minimum = &v
		}
		if v, ok := deepResearchSchemaFloat64(normalized["maximum"]); ok {
			schema.Maximum = &v
		}
	case "boolean":
		schema.Type = genai.TypeBoolean
	case "null":
		schema.Type = genai.TypeNULL
	default:
		schema.Type = genai.TypeObject
		properties := deepResearchSchemaMap(normalized["properties"])
		if len(properties) > 0 {
			keys := make([]string, 0, len(properties))
			schema.Properties = make(map[string]*genai.Schema, len(properties))
			for key, child := range properties {
				keys = append(keys, key)
				schema.Properties[key] = deepResearchJSONSchemaToGenAISchema(child, depth+1)
			}
			sort.Strings(keys)
			schema.PropertyOrdering = keys
		}
		required := deepResearchSchemaStringSlice(normalized["required"])
		if len(required) > 0 {
			schema.Required = required
		}
		if v, ok := deepResearchSchemaInt64(normalized["minProperties"]); ok {
			schema.MinProperties = &v
		}
		if v, ok := deepResearchSchemaInt64(normalized["maxProperties"]); ok {
			schema.MaxProperties = &v
		}
	}

	if enumVals := deepResearchSchemaEnum(normalized["enum"]); len(enumVals) > 0 {
		schema.Enum = enumVals
	}
	return schema
}

func deepResearchNormalizeJSONSchemaNode(node map[string]any) (map[string]any, bool) {
	normalized := make(map[string]any, len(node))
	for key, value := range node {
		normalized[key] = value
	}

	nullable := false
	if anyOfRaw, ok := node["anyOf"].([]any); ok {
		for _, item := range anyOfRaw {
			switch typed := item.(type) {
			case map[string]any:
				typeName := strings.ToLower(strings.TrimSpace(deepResearchSchemaString(typed["type"])))
				if typeName == "null" {
					nullable = true
					continue
				}
				for key, value := range typed {
					if _, exists := normalized[key]; !exists {
						normalized[key] = value
					}
				}
			case string:
				if strings.EqualFold(strings.TrimSpace(typed), "null") {
					nullable = true
				}
			}
		}
	}

	if rawTypes, ok := normalized["type"].([]any); ok {
		selected := ""
		for _, item := range rawTypes {
			typeName := strings.ToLower(strings.TrimSpace(deepResearchSchemaString(item)))
			if typeName == "" {
				continue
			}
			if typeName == "null" {
				nullable = true
				continue
			}
			if selected == "" {
				selected = typeName
			}
		}
		if selected != "" {
			normalized["type"] = selected
		} else {
			normalized["type"] = "null"
		}
	}
	if strings.EqualFold(strings.TrimSpace(deepResearchSchemaString(normalized["type"])), "null") {
		nullable = true
	}
	return normalized, nullable
}

func deepResearchJSONSchemaTypeName(node map[string]any) string {
	typeName := strings.ToLower(strings.TrimSpace(deepResearchSchemaString(node["type"])))
	if typeName != "" {
		return typeName
	}
	if len(deepResearchSchemaMap(node["properties"])) > 0 {
		return "object"
	}
	if _, ok := node["items"]; ok {
		return "array"
	}
	if len(deepResearchSchemaEnum(node["enum"])) > 0 {
		return "string"
	}
	return "object"
}

func deepResearchSchemaMap(value any) map[string]any {
	out, ok := value.(map[string]any)
	if !ok || out == nil {
		return nil
	}
	return out
}

func deepResearchSchemaString(value any) string {
	s, _ := value.(string)
	return s
}

func deepResearchSchemaStringSlice(value any) []string {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		s := strings.TrimSpace(deepResearchSchemaString(row))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func deepResearchSchemaEnum(value any) []string {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		s := strings.TrimSpace(fmt.Sprintf("%v", row))
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func deepResearchSchemaInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case json.Number:
		i, err := typed.Int64()
		if err == nil {
			return i, true
		}
		f, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return int64(f), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func deepResearchSchemaFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
