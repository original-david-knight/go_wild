package main

import (
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// commandResult indicates what the main loop should do after a command.
type commandResult int

const (
	cmdContinue       commandResult = iota // Continue to next input
	cmdProcessMessage                      // Process as normal message (command not handled)
	cmdRestart                             // Restart session with warning to agent
	cmdWorkTasks                           // Start working on tasks
)

// cmdArg reads a string arg from cm.Args with empty-string default.
func cmdArg(cm data.CommandMessage, key string) string {
	v, _ := cm.Args[key].(string)
	return v
}

type commandContext struct {
	history      *[]loop.Message
	pendingImage **imageAttachment
	agent        *loop.AgenticLoop
	smartMode    *bool
	models       modelPair
}

type commandHandler func(cm data.CommandMessage, ctx commandContext) commandResult

var commandHandlers = registerCommandHandlers()

func registerCommandHandlers() map[string]commandHandler {
	handlers := map[string]commandHandler{}
	add := func(handler commandHandler, names ...string) {
		for _, name := range names {
			handlers[name] = handler
		}
	}

	add(handleHelpCommand, "help", "h", "?")
	add(handleClearCommand, "clear", "c")
	add(handleRestartCommand, "restart", "r")
	add(handleHistoryCommand, "history")
	add(handleReportCommand, "report")
	add(handleContextCommand, "context", "ctx")
	add(handleContextDumpCommand, "contextdump", "dumpcontext", "ctxdump")
	add(handleTasksCommand, "tasks")
	add(handleAddTaskCommand, "addtask")
	add(handleFinishedCommand, "finished")
	add(handleWorkTasksCommand, "worktasks")
	add(handleStopTasksCommand, "stoptasks")
	add(handleRecurringListCommand, "recurring", "listrecurring")
	add(handleAddRecurringCommand, "addrecurring")
	add(handleDeleteRecurringCommand, "deleterecurring")
	add(handleSmartCommand, "smart")
	add(handleImageCommand, "image")
	add(handlePasteCommand, "paste")
	add(handleFileCommand, "file", "f")
	add(handleExitCommand, "exit", "quit", "q")
	add(handleTelegramCommand, "telegram", "settelegram")
	add(handleEmailCommand, "email", "setemail")
	add(handleOutboxCommand, "outbox")
	add(handleApproveCommand, "approve")
	add(handleRejectCommand, "reject")

	return handlers
}
