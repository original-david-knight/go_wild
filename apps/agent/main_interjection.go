package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// runAgentWithInterjection runs the agent and handles user interjections during execution.
// If the user types something while the agent is running, it immediately interrupts the current
// run and processes the interjection.
func runAgentWithInterjection(
	ctx context.Context,
	agent *loop.AgenticLoop,
	history []loop.Message,
	inputCh chan inputResult,
	startInputReader func(),
	pendingImage **imageAttachment,
	smartMode *bool,
	models modelPair,
	deferredHeartbeats *[]string,
) agentResult {
	history = compactHistoryAndReport(
		ctx,
		history,
		estimateHistoryTokens(history),
		*compactAt,
		*compactAfter,
		*keepRecentOutputs,
		models.base,
		"pre-run",
	)

	var assistantText strings.Builder
	var fullText strings.Builder
	var tokensUsed int
	var hitContextLimit bool
	var showingThinking bool
	var interrupted bool
	var lastError string

	// Create a child context so we can cancel this run on interjection
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	events := agent.Run(runCtx, history)

	for {
		select {
		case ir := <-inputCh:
			// User typed something during execution
			if ir.err == nil && ir.line != "" {
				// Queue heartbeat messages while agent is busy so they are processed
				// immediately after this run instead of being dropped.
				if hbMessage, ok := heartbeatMessageFromInput(ir.line); ok {
					if deferredHeartbeats != nil {
						*deferredHeartbeats = append(*deferredHeartbeats, hbMessage)
					}
					startInputReader()
					continue
				}

				if strings.HasPrefix(ir.line, "{") {
					// Allow smart-mode and status commands without interrupting the run.
					var base struct {
						Type string `json:"type"`
					}
					if err := json.Unmarshal([]byte(ir.line), &base); err == nil && base.Type != "" {
						switch base.Type {
						case "status_request":
							var req data.StatusRequestMessage
							if err := json.Unmarshal([]byte(ir.line), &req); err == nil {
								handleStatusRequest(req, *smartMode, models)
								startInputReader()
								continue
							}
						case "command":
							var cm data.CommandMessage
							if err := json.Unmarshal([]byte(ir.line), &cm); err == nil && cm.Command != "" {
								if strings.ToLower(strings.TrimSpace(cm.Command)) == "smart" {
									dispatchCommand(cm, &history, pendingImage, agent, smartMode, models)
									startInputReader()
									continue
								}
							}
						case "heartbeat":
							// Fallback: heartbeatMessageFromInput failed but type-switch caught it.
							var fullHB struct {
								Type    string `json:"type"`
								Message string `json:"message"`
							}
							if err := json.Unmarshal([]byte(ir.line), &fullHB); err == nil && strings.TrimSpace(fullHB.Message) != "" {
								if deferredHeartbeats != nil {
									*deferredHeartbeats = append(*deferredHeartbeats, strings.TrimSpace(fullHB.Message))
								}
								startInputReader()
								continue
							}
						}
					}
				}

				// Check for exit command - interrupt and exit immediately
				isExit := false
				if strings.HasPrefix(ir.line, "{") {
					var cm data.CommandMessage
					if err := json.Unmarshal([]byte(ir.line), &cm); err == nil {
						cmd := strings.ToLower(cm.Command)
						isExit = cmd == "exit" || cmd == "quit" || cmd == "q"
					}
				} else if cmdStr, ok := strings.CutPrefix(ir.line, "/"); ok {
					cmd := strings.ToLower(strings.Fields(cmdStr)[0])
					isExit = cmd == "exit" || cmd == "quit" || cmd == "q"
				}
				if isExit {
					if globalReadline != nil {
						globalReadline.Close()
					}
					fmt.Println("\nGoodbye!")
					os.Exit(0)
				}

				// Interjection received - cancel current run and process immediately
				if showingThinking {
					fmt.Print("\r            \r")
					showingThinking = false
				}
				fmt.Println(color.HiBlackString("\n(interjecting...)"))
				interrupted = true
				cancelRun()

				// Drain remaining events
				for range events {
				}

				// Add partial response to history if any
				if fullText.Len() > 0 {
					history = append(history, loop.NewModelTextMessage(fullText.String()))
				}

				// Add interjection and recurse immediately
				history = append(history, loop.NewUserMessage(ir.line))
				startInputReader()
				return runAgentWithInterjection(ctx, agent, history, inputCh, startInputReader, pendingImage, smartMode, models, deferredHeartbeats)
			}
			// Start reading next input
			startInputReader()

		case event, ok := <-events:
			if !ok {
				// Agent finished
				goto agentDone
			}

			if globalSessionLogger != nil {
				globalSessionLogger.LogEvent(event)
			}

			switch e := event.(type) {
			case loop.ThinkingEvent:
				output.Thinking()
				showingThinking = true

			case loop.TextDeltaEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				assistantText.WriteString(e.Text)
				fullText.WriteString(e.Text)
				output.Response(e.Text)

			case loop.ToolCallEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				toolCallCounts[e.Name]++
				// Build detail string
				var detail string
				if e.Name == "web_search" {
					detail, _ = e.Input["query"].(string)
				} else if url, ok := e.Input["url"].(string); ok {
					detail = url
				} else if len(e.Input) > 0 {
					if b, err := json.Marshal(e.Input); err == nil {
						detail = string(b)
						if len(detail) > 200 {
							detail = detail[:200] + "..."
						}
					}
				}
				output.ToolCall(e.Name, detail)

			case loop.ToolResultEvent:
				output.ToolResult(e.Name, e.Result.Success, e.Result.Error)

			case loop.DoneEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				tokensUsed = e.Usage.TotalTokens
				if len(e.History) > 0 {
					history = e.History
				}
				output.ResponseEnd(e.Usage.TotalTokens)

			case loop.CompactionEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				output.Compaction(e.MessagesCompacted, 0, e.PromptTokensBefore, e.PromptTokensAfter)

			case loop.ContextLimitEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				output.SystemWarning("Context approaching limit (%d/%d tokens)", e.PromptTokens, e.MaxTokens)
				hitContextLimit = true

			case loop.ErrorEvent:
				if showingThinking {
					output.ThinkingDone()
					showingThinking = false
				}
				// Don't print error if we caused it by interrupting
				if !interrupted && !errors.Is(e.Err, context.Canceled) {
					output.Error("Error: %v", e.Err)
				}
				if e.Err != nil {
					lastError = e.Err.Error()
				}
			}
		}
	}

agentDone:
	// Suppress unused variable warning - assistantText is used for building response
	_ = assistantText
	return agentResult{
		History:      history,
		TokensUsed:   tokensUsed,
		ContextLimit: hitContextLimit,
		FinalText:    fullText.String(),
		LastError:    lastError,
	}
}
