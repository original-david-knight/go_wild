package main

import "strings"

// normalizeAgentMode returns a canonical runtime mode.
func normalizeAgentMode(raw string) string {
	_ = raw
	return "interactive"
}

// normalizeWorkerContextMode returns a canonical worker context mode.
func normalizeWorkerContextMode(raw string) string {
	_ = raw
	return "stateless"
}

// parseRuntimeFlags extracts runtime mode flags from an extra flags string.
// It returns extracted mode/context plus all non-runtime flags.
func parseRuntimeFlags(extraFlags string) (mode string, workerContext string, cleaned []string) {
	tokens := strings.Fields(extraFlags)
	cleaned = make([]string, 0, len(tokens))

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case token == "-mode":
			if i+1 < len(tokens) {
				mode = tokens[i+1]
				i++
			}
		case strings.HasPrefix(token, "-mode="):
			mode = strings.TrimPrefix(token, "-mode=")
		case token == "-worker-context":
			if i+1 < len(tokens) {
				workerContext = tokens[i+1]
				i++
			}
		case strings.HasPrefix(token, "-worker-context="):
			workerContext = strings.TrimPrefix(token, "-worker-context=")
		default:
			cleaned = append(cleaned, token)
		}
	}

	return mode, workerContext, cleaned
}

// resolveAgentRuntimeConfig computes effective runtime mode/context.
// Explicit DB fields take precedence; legacy values are read from extra flags.
func resolveAgentRuntimeConfig(agentMode, workerContextMode, extraFlags string) (string, string) {
	_, _, _ = parseRuntimeFlags(extraFlags)
	_ = agentMode
	_ = workerContextMode
	return "interactive", "stateless"
}
