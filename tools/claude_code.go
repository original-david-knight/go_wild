package tools

// ClaudeCodeInput defines the input for running Claude Code.
// Execution happens on the manager side via the broker (see
// apps/agent_manager/broker_tools_claude_code.go); this package only
// retains the shared input shape consumed by the broker tool wrapper.
type ClaudeCodeInput struct {
	Prompt          string `json:"prompt" description:"Coding prompt to send to Claude Code via -p" required:"true"`
	TargetDirectory string `json:"target_directory" description:"Target directory to run Claude Code in. Relative paths are resolved under /data; absolute paths are mapped into /data unless already under /data." required:"false"`
	Timeout         int    `json:"timeout" description:"Timeout in seconds (default 600, max 3600)" required:"false"`
}
