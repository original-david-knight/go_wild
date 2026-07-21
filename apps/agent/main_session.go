package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	brokerclient "github.com/original-david-knight/go_wild/tools/broker"
)

func runInteractiveSession(ctx context.Context, sigCh chan os.Signal, agent *loop.AgenticLoop, agentID string, models modelPair, smartMode *bool) error {
	// Setup readline with autocomplete
	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/clear"),
		readline.PcItem("/restart"),
		readline.PcItem("/history"),
		readline.PcItem("/report"),
		readline.PcItem("/context"),
		readline.PcItem("/tasks"),
		readline.PcItem("/addtask"),
		readline.PcItem("/finished"),
		readline.PcItem("/worktasks"),
		readline.PcItem("/stoptasks"),
		readline.PcItem("/addrecurring"),
		readline.PcItem("/deleterecurring"),
		readline.PcItem("/recurring"),
		readline.PcItem("/listrecurring"),
		readline.PcItem("/smart"),
		readline.PcItem("/telegram"),
		readline.PcItem("/outbox"),
		readline.PcItem("/approve",
			readline.PcItem("all")),
		readline.PcItem("/reject",
			readline.PcItem("all")),
		readline.PcItem("/image",
			readline.PcItemDynamic(completeFilePath)),
		readline.PcItem("/file",
			readline.PcItemDynamic(completeFilePath)),
		readline.PcItem("/paste"),
		readline.PcItem("/exit"),
	)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "",
		HistoryFile:     getHistoryFile(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    completer,
		Stdout:          io.Discard,
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	globalReadline = rl
	defer rl.Close()

	// Set up signal handler now that readline exists
	go func() {
		<-sigCh
		rl.Close() // Restore terminal before printing
		if globalTelegramTools != nil {
			globalTelegramTools.Stop()
		}
		fmt.Println("\nGoodbye!")
		os.Exit(0)
	}()

	// Print welcome message
	printWelcome(agentID)

	// Conversation history
	var history []loop.Message
	if rehydrated, err := loadHistorySnapshot(ctx); err != nil {
		output.SystemWarning("Rehydrate failed: %v", err)
	} else if len(rehydrated) > 0 {
		history = rehydrated
		output.System("Rehydrated %d history messages", len(history))
	}

	// Current context size in tokens (from latest API response)
	var contextTokens int

	// compactIfNeeded checks context size after a run and trims history toward the
	// configured steady-state target.
	compactIfNeeded := func() {
		history = compactHistoryAndReport(
			ctx,
			history,
			contextTokens,
			*compactAfter,
			*compactAfter,
			*keepRecentOutputs,
			models.base,
			"post-run",
		)
	}

	persistHistory := func() {
		if err := saveHistorySnapshot(ctx, history); err != nil {
			output.SystemWarning("History persist failed: %v", err)
		}
	}

	// Pending image for next message
	var pendingImage *imageAttachment

	// Smart mode state - initialize from flag
	if *smartMode {
		agent.SetModel(models.smart)
		agent.SetThinkingBudget(smartThinkingBudget)
		output.SmartMode(true, models.smart)
		output.System("Smart mode ON")
		output.System("  Model: %s", models.smart)
		output.System("  Thinking budget: %d tokens", smartThinkingBudget)
	} else {
		output.SmartMode(false, models.base)
	}

	// Async input reader - single goroutine reads input continuously
	inputCh := make(chan inputResult, 1)
	inputReader := newAsyncInputReader(rl, output.Prompt, inputCh)
	startInputReader := inputReader.Start

	// Start first input read
	startInputReader()

	// Heartbeats received while the agent is busy are queued and processed
	// before regular task-loop work on the next iteration.
	var deferredHeartbeats []string

	// Heartbeat timer setup
	var heartbeatTimer *time.Timer
	var heartbeatCh <-chan time.Time
	resetHeartbeat := func() {
		if *heartbeatInterval > 0 {
			if heartbeatTimer != nil {
				heartbeatTimer.Stop()
			}
			heartbeatTimer = time.NewTimer(*heartbeatInterval)
			heartbeatCh = heartbeatTimer.C
		}
	}
	resetHeartbeat()

	if *heartbeatInterval > 0 {
		output.System("Heartbeat: %v", *heartbeatInterval)
	}

	// Main REPL loop
	for {
		// Wait for user input or heartbeat
		var input string
		var isHeartbeat bool
		var claimedMethodJobID string
		var claimedMethodName string
		var claimedMethodFreshContext bool

		if len(deferredHeartbeats) > 0 {
			input = deferredHeartbeats[0]
			deferredHeartbeats = deferredHeartbeats[1:]
			isHeartbeat = true
			if methodCall, ok := claimedMethodCallConfigFromHeartbeat(input); ok {
				claimedMethodJobID = methodCall.JobID
				claimedMethodName = methodCall.Method
				claimedMethodFreshContext = methodCall.FreshContext
			}
			output.TaskPrompt(input)
			resetHeartbeat()
		} else {
			// Work Tasks Mode: check for next task before waiting for input
			if workTasksMode {
				// Check for timeout
				elapsed := time.Since(workTasksStartTime)
				if *workTasksTimeout > 0 && elapsed >= *workTasksTimeout {
					workTasksMode = false
					output.SystemWarning("Work Tasks timeout (%v elapsed) - taking a break", elapsed.Round(time.Second))
					output.System("Use /worktasks to resume")
					startInputReader()
					continue
				}

				output.System("[DEBUG] workTasksMode=true, checking for next task... (%v elapsed)", elapsed.Round(time.Second))
				nextPrompt := generateWorkTasksPrompt()
				if nextPrompt == "" {
					output.System("[DEBUG] no more tasks, disabling workTasksMode")
					workTasksMode = false
					output.SystemSuccess("All tasks completed! Work Tasks Mode disabled.")
				} else {
					output.System("[DEBUG] found next task, running agent")
					fmt.Println()
					output.TaskPrompt(nextPrompt)
					history = append(history, loop.NewUserMessage(nextPrompt))
					// Don't call startInputReader() here - one is already running from previous iteration
					// The runAgentWithInterjection will still handle user interjections via inputCh
					result := runAgentWithInterjection(ctx, agent, history, inputCh, startInputReader, &pendingImage, smartMode, models, &deferredHeartbeats)
					history = result.History
					contextTokens = result.TokensUsed
					output.ContextInfo(contextTokens, *compactAfter)
					compactIfNeeded()
					persistHistory()
					resetHeartbeat()
					output.System("[DEBUG] agent finished, continuing loop")
					continue
				}
			}

			select {
			case ir := <-inputCh:
				if ir.err != nil {
					if ir.err == readline.ErrInterrupt {
						resetHeartbeat()
						startInputReader()
						continue
					}
					if ir.err == io.EOF {
						fmt.Println("\nGoodbye!")
						return nil
					}
					return ir.err
				}
				input = ir.line
				resetHeartbeat()

				if input == "" {
					startInputReader()
					continue
				}

				// Handle structured JSON commands (from manager relay) or text commands
				var cmdResult commandResult
				handled := false
				if hbMessage, ok := heartbeatMessageFromInput(input); ok {
					input = hbMessage
					isHeartbeat = true
					if methodCall, ok := claimedMethodCallConfigFromHeartbeat(input); ok {
						claimedMethodJobID = methodCall.JobID
						claimedMethodName = methodCall.Method
						claimedMethodFreshContext = methodCall.FreshContext
					}
					output.TaskPrompt(input)
					resetHeartbeat()
				} else if strings.HasPrefix(input, "{") {
					var base struct {
						Type string `json:"type"`
					}
					if err := json.Unmarshal([]byte(input), &base); err == nil && base.Type != "" {
						switch base.Type {
						case "command":
							var cm data.CommandMessage
							if err := json.Unmarshal([]byte(input), &cm); err == nil && cm.Command != "" {
								cmdResult = dispatchCommand(cm, &history, &pendingImage, agent, smartMode, models)
								handled = true
							}
						case "status_request":
							var req data.StatusRequestMessage
							if err := json.Unmarshal([]byte(input), &req); err == nil {
								handleStatusRequest(req, *smartMode, models)
								cmdResult = cmdContinue
								handled = true
							}
						case "heartbeat":
							// Fallback: heartbeatMessageFromInput failed (encoding issue?)
							// but JSON type-switch still detected it. Extract the message.
							var fullHB struct {
								Type    string `json:"type"`
								Message string `json:"message"`
							}
							if err := json.Unmarshal([]byte(input), &fullHB); err == nil && strings.TrimSpace(fullHB.Message) != "" {
								input = strings.TrimSpace(fullHB.Message)
								isHeartbeat = true
								if methodCall, ok := claimedMethodCallConfigFromHeartbeat(input); ok {
									claimedMethodJobID = methodCall.JobID
									claimedMethodName = methodCall.Method
									claimedMethodFreshContext = methodCall.FreshContext
								}
								output.TaskPrompt(input)
								resetHeartbeat()
							}
						}
					} else {
						var cm data.CommandMessage
						if err := json.Unmarshal([]byte(input), &cm); err == nil && cm.Command != "" {
							cmdResult = dispatchCommand(cm, &history, &pendingImage, agent, smartMode, models)
							handled = true
						}
					}
				}
				if !handled && strings.HasPrefix(input, "/") {
					cmdResult = handleCommand(input, &history, &pendingImage, agent, smartMode, models)
					handled = true
				}
				if handled {
					switch cmdResult {
					case cmdContinue:
						startInputReader()
						continue
					case cmdRestart:
						// Warn the agent and let it respond before clearing
						now := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")
						restartMsg := fmt.Sprintf("Prepare for a restart. The user has requested to clear the conversation. Current time: %s", now)
						output.SystemWarning("%s", restartMsg)

						history = append(history, loop.NewUserMessage(restartMsg))
						startInputReader()
						_ = runAgentWithInterjection(ctx, agent, history, inputCh, startInputReader, &pendingImage, smartMode, models, &deferredHeartbeats)

						// Clear history for fresh start
						history = nil
						contextTokens = 0
						persistHistory()
						output.SystemSuccess("Session restarted with fresh context")
						resetHeartbeat()
						fmt.Println()
						continue
					case cmdWorkTasks:
						// Generate work tasks prompt and fall through
						input = consumePreparedWorkTasksPrompt()
						if input == "" {
							workTasksMode = false
							output.SystemSuccess("All tasks completed!")
							startInputReader()
							continue
						}
						output.TaskPrompt(input)
					case cmdProcessMessage:
						// Fall through to process as normal message
					}
				}

			case <-heartbeatCh:
				isHeartbeat = true
				now := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")

				// Check for pending Telegram messages
				telegramMsgCount := 0
				if globalTelegramTools != nil {
					telegramMsgCount = globalTelegramTools.PendingMessageCount()
				}

				status, err := checkWorkTasks(ctx)
				if status.recurringErr != nil {
					output.SystemWarning("Recurring task check failed: %v", status.recurringErr)
				}
				if err != nil {
					output.SystemWarning("Error getting tasks: %v", err)
					resetHeartbeat()
					continue
				}
				if status.created > 0 {
					output.System("Created %d task(s) from recurring templates", status.created)
				}

				// Only send heartbeat if there are pending tasks or Telegram messages.
				if status.pendingCount == 0 && telegramMsgCount == 0 {
					// Nothing to do, skip heartbeat silently
					resetHeartbeat()
					continue
				}

				// Build heartbeat prompt
				var heartbeatParts []string

				// Add Telegram notification if messages pending
				if telegramMsgCount > 0 {
					output.System("%d Telegram message(s) waiting", telegramMsgCount)
					heartbeatParts = append(heartbeatParts, fmt.Sprintf("You have %d new Telegram message(s). Use telegram_get_updates to read and respond.", telegramMsgCount))
				}

				if status.pendingCount > 0 {
					if status.blockedMessage != "" {
						heartbeatParts = append(heartbeatParts, formatBlockedHeartbeat(status.nonWorkableKind, status.blockedMessage))
					} else if status.prompt != "" {
						// Enable work tasks mode so we continue processing tasks after this one
						workTasksMode = true
						workTasksStartTime = time.Now()
						output.System("Work Tasks Mode enabled (heartbeat triggered)")
						heartbeatParts = append(heartbeatParts, status.prompt)
					}
				}

				autonomousReminder := "You are running autonomously — there is no human to interact with. Do not ask questions or wait for input; decide and act on your own."
				if len(heartbeatParts) > 0 {
					input = fmt.Sprintf("Heartbeat: Current time is %s. %s\n\n%s", now, autonomousReminder, strings.Join(heartbeatParts, " "))
				} else {
					input = fmt.Sprintf("Heartbeat: Current time is %s. %s", now, autonomousReminder)
				}
				output.TaskPrompt(input)
				resetHeartbeat()
			}
		}

		// Log the prompt being sent
		if !isHeartbeat {
			output.emit(OutputMessage{Type: MsgPrompt, Content: input})
		}

		runHistory := history
		if isHeartbeat {
			runHistory = historyForClaimedMethodCall(history, input, claimedMethodFreshContext && strings.TrimSpace(claimedMethodJobID) != "")
		} else if pendingImage != nil {
			runHistory = append(runHistory, loop.NewUserMessageWithImage(input, pendingImage.data, pendingImage.mimeType))
			output.System("(attached: %s, %d bytes)", pendingImage.name, len(pendingImage.data))
			pendingImage = nil // Clear after use
		} else {
			runHistory = append(runHistory, loop.NewUserMessage(input))
		}

		// Start reading next input while agent runs
		startInputReader()

		// Run the agent with interjection support
		runCtx := ctx
		if isHeartbeat && strings.TrimSpace(claimedMethodJobID) != "" && strings.TrimSpace(claimedMethodName) != "" {
			runCtx = brokerclient.WithExecutionMethod(runCtx, claimedMethodName)
		}
		result := runAgentWithInterjection(runCtx, agent, runHistory, inputCh, startInputReader, &pendingImage, smartMode, models, &deferredHeartbeats)
		history = finalizeClaimedMethodCallHistory(history, result.History, claimedMethodFreshContext && strings.TrimSpace(claimedMethodJobID) != "")
		if isHeartbeat && strings.TrimSpace(claimedMethodJobID) != "" {
			autoCompleteClaimedMethodJob(ctx, claimedMethodJobID, result.FinalText, result.LastError)
		}
		// Use latest token count as context size (not cumulative - each call includes full history)
		if !(claimedMethodFreshContext && strings.TrimSpace(claimedMethodJobID) != "") {
			contextTokens = result.TokensUsed
		}
		output.ContextInfo(result.TokensUsed, *compactAfter)

		// Show pending email count if any
		if globalEmailOutbox != nil {
			if count := globalEmailOutbox.PendingCount(context.Background()); count > 0 {
				output.SystemWarning("%d email(s) pending approval - type /outbox to review", count)
			}
		}

		resetHeartbeat() // Reset after agent finishes

		if !(claimedMethodFreshContext && strings.TrimSpace(claimedMethodJobID) != "") {
			compactIfNeeded()
			persistHistory()
		}

		output.System("[DEBUG] end of loop iteration, workTasksMode=%v", workTasksMode)
	}
}

func handleStatusRequest(req data.StatusRequestMessage, smartMode bool, models modelPair) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	switch name {
	case "smart_mode", "smart":
		model := models.base
		if smartMode {
			model = models.smart
		}
		output.SmartMode(smartMode, model)
	case "runtime_status":
		model := models.base
		if smartMode {
			model = models.smart
		}
		output.RuntimeStatus(data.RuntimeStatus{
			State:     "idle",
			SmartMode: smartMode,
			Model:     model,
		})
	}
}
