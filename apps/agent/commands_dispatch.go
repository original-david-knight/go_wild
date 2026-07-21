package main

import (
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/fatih/color"
)

// parseTextCommand converts a text command like "/addtask foo bar" into a CommandMessage.
func parseTextCommand(input string) *data.CommandMessage {
	parts := strings.Fields(input[1:]) // Remove leading /
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	args := map[string]any{}
	rest := strings.Join(parts[1:], " ")

	switch cmd {
	case "addtask":
		if rest != "" {
			args["description"] = rest
		}
	case "addrecurring":
		if len(parts) > 1 {
			if looksLikeInterval(parts[1]) {
				args["interval"] = parts[1]
				if len(parts) > 2 {
					args["description"] = strings.Join(parts[2:], " ")
				}
			} else {
				args["description"] = rest
			}
		}
	case "deleterecurring":
		if len(parts) > 1 {
			args["id"] = parts[1]
		}
	case "image", "file", "f":
		if rest != "" {
			args["path"] = rest
		}
	case "approve", "reject":
		if len(parts) > 1 {
			args["id"] = parts[1]
		}
	case "telegram", "settelegram":
		if len(parts) > 1 {
			args["value"] = parts[1]
		}
	case "email", "setemail":
		if len(parts) > 1 {
			if strings.ToLower(parts[1]) == "apikey" {
				args["subcommand"] = "apikey"
				if len(parts) > 2 {
					args["value"] = parts[2]
				}
			} else {
				args["value"] = parts[1]
			}
		}
	}

	return &data.CommandMessage{
		Type:    "command",
		Command: cmd,
		Args:    args,
		Raw:     input,
	}
}

// handleCommand parses a text command and dispatches it.
func handleCommand(input string, history *[]loop.Message, pendingImage **imageAttachment, agent *loop.AgenticLoop, smartMode *bool, models modelPair) commandResult {
	cm := parseTextCommand(input)
	if cm == nil {
		return cmdContinue
	}
	return dispatchCommand(*cm, history, pendingImage, agent, smartMode, models)
}

// dispatchCommand executes a structured command.
func dispatchCommand(cm data.CommandMessage, history *[]loop.Message, pendingImage **imageAttachment, agent *loop.AgenticLoop, smartMode *bool, models modelPair) commandResult {
	handler := commandHandlers[cm.Command]
	if handler == nil {
		fmt.Println(color.RedString("Unknown command: %s (type /help for commands)", cm.Command))
		return cmdContinue
	}
	ctx := commandContext{
		history:      history,
		pendingImage: pendingImage,
		agent:        agent,
		smartMode:    smartMode,
		models:       models,
	}
	return handler(cm, ctx)
}
