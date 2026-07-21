package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/fatih/color"
)

func handleExitCommand(_ data.CommandMessage, _ commandContext) commandResult {
	// Warn about pending emails
	if globalEmailOutbox != nil {
		if count := globalEmailOutbox.PendingCount(context.Background()); count > 0 {
			fmt.Println(color.YellowString("Warning: %d email(s) pending approval. Use /outbox to review before exiting.", count))
			fmt.Print("Exit anyway? [y/N] ")
			var confirm string
			fmt.Scanln(&confirm)
			if strings.ToLower(confirm) != "y" {
				return cmdContinue
			}
		}
	}
	if globalReadline != nil {
		globalReadline.Close()
	}
	fmt.Println("Goodbye!")
	os.Exit(0)
	return cmdContinue
}
