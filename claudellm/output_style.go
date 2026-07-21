package claudellm

import (
	"os"
	"sync"
)

// SandboxOutputStylePath is the path inside the bwrap sandbox where the
// output style file is bind-mounted.
const SandboxOutputStylePath = "/sandbox/config/output-style.md"

// researchOutputStyleContent is a Claude Code output style that strips the
// default software engineering system prompt. Used for research, analysis,
// and structured mission execution — everything except actual coding tasks.
const researchOutputStyleContent = `---
name: Research Agent
description: Research and analysis agent for structured missions
keep-coding-instructions: false
---

You are a research and analysis agent executing structured missions.

Use the available tools to gather data, analyze information, and produce results. Return your final answer as a clean JSON object. Do not wrap JSON in markdown code fences. Do not include commentary outside the JSON.
`

// WriteResearchOutputStyle writes the research output style to a temporary
// file and returns its path and a cleanup function.
func WriteResearchOutputStyle() (string, func(), error) {
	f, err := os.CreateTemp("", "claude-output-style-*.md")
	if err != nil {
		return "", nil, err
	}
	path := f.Name()
	cleanup := func() { os.Remove(path) }

	if _, err := f.WriteString(researchOutputStyleContent); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	f.Close()
	return path, cleanup, nil
}

var (
	researchStyleOnce sync.Once
	researchStylePath string
)

// ResearchOutputStylePath returns a process-lifetime path to the research
// output style file. Safe for concurrent use from long-lived clients.
func ResearchOutputStylePath() string {
	researchStyleOnce.Do(func() {
		path, _, err := WriteResearchOutputStyle()
		if err != nil {
			panic("failed to write research output style: " + err.Error())
		}
		// Intentionally no cleanup — lives for process lifetime.
		researchStylePath = path
	})
	return researchStylePath
}
