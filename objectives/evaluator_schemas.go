package objectives

import (
	"google.golang.org/genai"
)

// evaluationSchema returns the Gemini structured output schema for post-execution evaluation.
func evaluationSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"sufficient": {
				Type:        genai.TypeBoolean,
				Description: "Whether the task achieved its goal",
			},
			"reasoning": {
				Type:        genai.TypeString,
				Description: "Detailed explanation of the evaluation judgment",
			},
			"extracted_facts": {
				Type:        genai.TypeArray,
				Description: "Facts discovered during execution that should be remembered",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"fact": {
							Type:        genai.TypeString,
							Description: "The discovered fact",
						},
						"tags": {
							Type:        genai.TypeArray,
							Items:       &genai.Schema{Type: genai.TypeString},
							Description: "Categorization tags for this fact",
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: "Confidence in this fact from 0.0 to 1.0",
						},
						"expires_in_days": {
							Type:        genai.TypeInteger,
							Description: "Number of days until this fact should expire (0 = never)",
						},
					},
					Required: []string{"fact", "tags", "confidence"},
				},
			},
			"decision_summary": {
				Type:        genai.TypeObject,
				Description: "Summary of the key decision made during this execution",
				Properties: map[string]*genai.Schema{
					"decision": {
						Type:        genai.TypeString,
						Description: "What was decided",
					},
					"reasoning": {
						Type:        genai.TypeString,
						Description: "Why this decision was made",
					},
					"outcome": {
						Type:        genai.TypeString,
						Description: "The result of the decision",
					},
				},
				Required: []string{"decision", "reasoning", "outcome"},
			},
			"replan_level": {
				Type:        genai.TypeString,
				Description: "How much replanning is needed based on execution results",
				Enum:        []string{"none", "reactive", "tactical", "strategic"},
			},
		},
		Required: []string{"sufficient", "reasoning", "replan_level"},
	}
}
