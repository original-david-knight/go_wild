package main

import (
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestCommandHandlersCoverage(t *testing.T) {
	commands := []string{
		"help", "h", "?",
		"clear", "c",
		"restart", "r",
		"history",
		"report",
		"context", "ctx",
		"tasks",
		"addtask",
		"finished",
		"worktasks",
		"stoptasks",
		"recurring", "listrecurring",
		"addrecurring",
		"deleterecurring",
		"smart",
		"image",
		"paste",
		"file", "f",
		"exit", "quit", "q",
		"telegram", "settelegram",
		"email", "setemail",
		"outbox",
		"approve",
		"reject",
	}

	handlers := registerCommandHandlers()
	for _, cmd := range commands {
		if handlers[cmd] == nil {
			t.Fatalf("expected handler for command %q", cmd)
		}
	}
}

func TestDispatchCommandUsesRegistry(t *testing.T) {
	orig := commandHandlers
	t.Cleanup(func() { commandHandlers = orig })

	commandHandlers = map[string]commandHandler{
		"custom": func(_ data.CommandMessage, _ commandContext) commandResult {
			return cmdRestart
		},
	}

	cm := data.CommandMessage{Command: "custom"}
	var history []loop.Message
	var pending *imageAttachment
	var smart bool
	got := dispatchCommand(cm, &history, &pending, nil, &smart, modelPair{})
	if got != cmdRestart {
		t.Fatalf("expected cmdRestart, got %v", got)
	}
}

func TestDispatchCommandUnknownReturnsContinue(t *testing.T) {
	orig := commandHandlers
	t.Cleanup(func() { commandHandlers = orig })
	commandHandlers = map[string]commandHandler{}

	cm := data.CommandMessage{Command: "does-not-exist"}
	got := dispatchCommand(cm, nil, nil, nil, nil, modelPair{})
	if got != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", got)
	}
}
