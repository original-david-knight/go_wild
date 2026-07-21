package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func handleHelpCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printHelp()
	return cmdContinue
}

func handleClearCommand(_ data.CommandMessage, ctx commandContext) commandResult {
	if ctx.history != nil {
		*ctx.history = nil
	}
	if ctx.pendingImage != nil {
		*ctx.pendingImage = nil
	}
	if err := saveHistorySnapshot(context.Background(), nil); err != nil {
		output.SystemWarning("History persist failed: %v", err)
	}
	output.System("Conversation cleared.")
	return cmdContinue
}

func handleRestartCommand(_ data.CommandMessage, ctx commandContext) commandResult {
	if ctx.history == nil || len(*ctx.history) == 0 {
		output.System("Nothing to restart - conversation is empty.")
		return cmdContinue
	}
	return cmdRestart
}

func handleHistoryCommand(_ data.CommandMessage, ctx commandContext) commandResult {
	if ctx.history == nil || len(*ctx.history) == 0 {
		output.System("No conversation history.")
	} else {
		output.System("Conversation history (%d messages):", len(*ctx.history))
		for i, msg := range *ctx.history {
			role := string(msg.Role)
			output.System("  %d. [%s] %s", i+1, role, truncate(loop.ExtractText(msg.Content), 50))
		}
	}
	return cmdContinue
}

func handleReportCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printToolReport()
	return cmdContinue
}

func handleContextCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printContext()
	return cmdContinue
}

// printHelp displays available commands and flags.
func printHelp() {
	fmt.Println(color.YellowString("Commands:"))
	fmt.Println("  /help, /h, /?  - Show this help")
	fmt.Println("  /clear, /c     - Clear conversation history (immediate)")
	fmt.Println("  /restart, /r   - Restart session (warns agent first, then clears)")
	fmt.Println("  /history       - Show conversation history")
	fmt.Println("  /context, /ctx - Show short-term memory")
	fmt.Println("  /contextdump   - Emit full agent context (history + tool outputs)")
	fmt.Println("  /report        - Show tool call statistics")
	fmt.Println("  /tasks         - Show pending tasks")
	fmt.Println("  /addtask <desc>- Add a task for the agent")
	fmt.Println("  /finished      - Show done and deprecated tasks")
	fmt.Println("  /worktasks     - Start working through pending tasks")
	fmt.Println("  /stoptasks     - Stop work tasks mode")
	fmt.Println("  /listrecurring - List recurring tasks (alias: /recurring)")
	fmt.Println("  /addrecurring [desc] - Add a recurring task (prompts for interval)")
	fmt.Println("  /deleterecurring - Delete a recurring task (interactive)")
	fmt.Println("  /smart         - Toggle smart mode (uses pro model + extended thinking)")
	fmt.Println("  /telegram <token> - Set Telegram bot token (from @BotFather)")
	fmt.Println("  /email apikey <key> - Set AgentMail API key")
	fmt.Println("  /email <inbox_id> - Set AgentMail inbox ID")
	fmt.Println("  /outbox        - Show pending outgoing emails awaiting approval")
	fmt.Println("  /approve <n>   - Approve and send a pending email (or /approve all)")
	fmt.Println("  /reject <n>    - Reject a pending email (or /reject all)")
	fmt.Println("  /file <path>   - Attach any file (PDF, code, text, etc.) with Tab completion")
	fmt.Println("  /image <path>  - Attach an image file to your next message")
	fmt.Println("  /paste         - Attach image from clipboard to your next message")
	fmt.Println("  /exit, /quit   - Exit the agent")
	fmt.Println()
	fmt.Println(color.YellowString("Flags:"))
	fmt.Println("  -agent <id>         - Agent ID (default: jake)")
	fmt.Println("  -provider <name>    - LLM provider (gemini, openai, anthropic)")
	fmt.Println("  -openai-auth <mode> - OpenAI auth mode (api_key or codex_oauth)")
	fmt.Println("  -model <name>       - Provider-specific model to use")
	fmt.Println("                       OpenAI defaults: OPEN_AI_FAST_MODEL and OPEN_AI_SMART_MODEL")
	fmt.Println("  -system <msg>       - Custom system prompt")
	fmt.Println("  -max-turns <n>      - Max agentic turns (default: 10)")
	fmt.Println("  -heartbeat <dur>    - Heartbeat interval (default: 15m, 0 to disable)")
	fmt.Println("  -worktasks-timeout <dur> - Work tasks timeout (default: 4m, 0 to disable)")
	fmt.Println("  -max-context <n>    - Soft limit: restart at heartbeat (default: 200000)")
	fmt.Println("  -hard-limit <n>     - Hard limit: immediate restart (default: 400000)")
	fmt.Println()
	fmt.Println(color.YellowString("Agent Management:"))
	fmt.Println("  -create-agent <name>  - Create a new agent")
	fmt.Println("  -seed-phrase <phrase> - Import wallet seed (with -create-agent)")
	fmt.Println("  -telegram-token <tok> - Set Telegram bot token (with -agent or -create-agent)")
	fmt.Println("  -email-apikey <key>   - Set AgentMail API key (with -agent or -create-agent)")
	fmt.Println("  -email-inbox <id>     - Set AgentMail inbox ID (with -agent or -create-agent)")
	fmt.Println("  -delete-agent <id>    - Delete an agent and all data")
	fmt.Println("  -list-agents          - List all agents")
	fmt.Println()
	fmt.Println(color.YellowString("Sandbox (Docker - default):"))
	fmt.Println("  -no-sandbox         - Run locally without Docker sandbox (debug mode)")
	fmt.Println("  -sandbox-bg         - Run sandbox in background (detached)")
	fmt.Println("  -sandbox-rebuild    - Force rebuild of the sandbox image")
	fmt.Println("  -sandbox-list       - List all sandbox containers")
	fmt.Println("  -sandbox-stop       - Stop an agent's sandbox")
	fmt.Println("  -sandbox-rm         - Remove an agent's sandbox (keeps data)")
	fmt.Println("  -sandbox-purge      - Remove sandbox AND data volume")
	fmt.Println("  -sandbox-logs       - Show logs from an agent's sandbox")
	fmt.Println("  -sandbox-follow     - Follow log output (use with -sandbox-logs)")
	fmt.Println("  -sandbox-exec <cmd> - Execute command in agent's sandbox")
}

// printToolReport displays tool call statistics.
func printToolReport() {
	if len(toolCallCounts) == 0 {
		fmt.Println(color.YellowString("No tool calls recorded yet."))
		return
	}

	// Calculate total
	total := 0
	for _, count := range toolCallCounts {
		total += count
	}

	fmt.Println(color.YellowString("Tool Call Report (%d total calls):", total))
	fmt.Println()

	// Sort by count (descending), then by name
	type toolCount struct {
		name  string
		count int
	}
	sorted := make([]toolCount, 0, len(toolCallCounts))
	for name, count := range toolCallCounts {
		sorted = append(sorted, toolCount{name, count})
	}
	// Sort: highest count first, then alphabetically
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count ||
				(sorted[j].count == sorted[i].count && sorted[j].name < sorted[i].name) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Find max name length for alignment
	maxLen := 0
	for _, tc := range sorted {
		if len(tc.name) > maxLen {
			maxLen = len(tc.name)
		}
	}

	// Print each tool with a bar chart
	for _, tc := range sorted {
		pct := float64(tc.count) / float64(total) * 100
		barLen := int(pct / 2) // Max 50 chars for 100%
		bar := strings.Repeat("█", barLen)
		fmt.Printf("  %-*s  %4d  %5.1f%%  %s\n", maxLen, tc.name, tc.count, pct, color.CyanString(bar))
	}
}

// printContext displays short-term memory.
func printContext() {
	ctx := context.Background()

	content, err := getShortTermMemory(ctx)
	if err != nil {
		fmt.Println(color.RedString("  Error loading memory: %v", err))
	} else if content != "" {
		fmt.Println(color.CyanString("  Short-Term Memory (%d chars):", len(content)))
		for _, line := range strings.Split(content, "\n") {
			fmt.Printf("    %s\n", line)
		}
	} else {
		fmt.Println(color.HiBlackString("  Short-term memory: (empty)"))
	}
}
