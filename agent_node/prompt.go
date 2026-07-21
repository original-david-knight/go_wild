package agentnode

import (
	"fmt"
	"strings"
)

// buildNodePrompt constructs the full prompt for a node by prepending
// dependency outputs from SharedState to the node's own prompt.
func buildNodePrompt(def *NodeDef, state *SharedState) string {
	if len(def.DependsOn) == 0 || state == nil {
		return def.Prompt
	}

	var contextParts []string
	for _, depID := range def.DependsOn {
		r := state.get(depID)
		if r == nil || r.Status != NodeDone {
			continue
		}
		var output string
		if len(r.Output) > 0 {
			output = string(r.Output)
		} else if r.Text != "" {
			output = r.Text
		}
		if output == "" {
			continue
		}
		contextParts = append(contextParts, fmt.Sprintf("### %s\n%s", depID, output))
	}

	if len(contextParts) == 0 {
		return def.Prompt
	}

	return fmt.Sprintf("## Context from prior nodes\n%s\n\n%s",
		strings.Join(contextParts, "\n\n"), def.Prompt)
}
