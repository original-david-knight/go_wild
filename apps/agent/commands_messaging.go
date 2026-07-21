package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
)

func handleTelegramCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	token := cmdArg(cm, "value")
	if token == "" {
		fmt.Println(color.YellowString("Usage: /telegram <bot_token>"))
		fmt.Println(color.HiBlackString("Get a token from @BotFather on Telegram"))
		if globalTelegramTools != nil {
			fmt.Println(color.HiBlackString("Current bot: @%s", globalTelegramTools.GetBotUsername()))
		}
		return cmdContinue
	}
	if err := setTelegramTokenInteractive(context.Background(), token, ctx.agent); err != nil {
		fmt.Println(color.RedString("Failed to set Telegram token: %v", err))
	}
	return cmdContinue
}

func handleEmailCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	subcmd := cmdArg(cm, "subcommand")
	value := cmdArg(cm, "value")
	if subcmd == "" && value == "" {
		fmt.Println(color.YellowString("Usage:"))
		fmt.Println("  /email apikey <key> - Set AgentMail API key")
		fmt.Println("  /email <inbox_id>   - Set AgentMail inbox ID")
		fmt.Println(color.HiBlackString("Get your key and inbox ID from https://console.agentmail.to"))
		if globalEmailTools != nil {
			fmt.Println(color.HiBlackString("Current inbox: %s", globalEmailTools.GetInboxID()))
		}
		return cmdContinue
	}
	if subcmd == "apikey" {
		if value == "" {
			fmt.Println(color.RedString("Usage: /email apikey <key>"))
			return cmdContinue
		}
		if err := setEmailAPIKeyInteractive(context.Background(), value, ctx.agent); err != nil {
			fmt.Println(color.RedString("Failed to set email API key: %v", err))
		}
	} else {
		inboxID := value
		if err := setEmailInboxInteractive(context.Background(), inboxID, ctx.agent); err != nil {
			fmt.Println(color.RedString("Failed to set email: %v", err))
		}
	}
	return cmdContinue
}

func handleOutboxCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printOutbox()
	return cmdContinue
}

func handleApproveCommand(cm data.CommandMessage, _ commandContext) commandResult {
	id := cmdArg(cm, "id")
	if id == "" {
		fmt.Println(color.RedString("Usage: /approve <n> or /approve all"))
		return cmdContinue
	}
	handleApprove(id)
	return cmdContinue
}

func handleRejectCommand(cm data.CommandMessage, _ commandContext) commandResult {
	id := cmdArg(cm, "id")
	if id == "" {
		fmt.Println(color.RedString("Usage: /reject <n> or /reject all"))
		return cmdContinue
	}
	handleReject(id)
	return cmdContinue
}

// Email outbox commands

func printOutbox() {
	if globalEmailOutbox == nil {
		fmt.Println(color.YellowString("Email not configured."))
		return
	}

	ctx := context.Background()
	pending, err := globalEmailOutbox.GetPending(ctx)
	if err != nil {
		fmt.Println(color.RedString("Error loading outbox: %v", err))
		return
	}

	if len(pending) == 0 {
		fmt.Println(color.YellowString("No pending outgoing emails."))
		return
	}

	fmt.Println(color.YellowString("Pending outgoing emails (%d):", len(pending)))
	fmt.Println()
	for i, pe := range pending {
		fmt.Printf("  %d. [%s] To: %s\n", i+1, pe.Type, pe.Recipients)
		fmt.Printf("     Subject: %s\n", pe.Subject)
		if pe.Preview != "" {
			fmt.Printf("     Preview: %s\n", color.HiBlackString(pe.Preview))
		}
		fmt.Printf("     Queued: %s\n", color.HiBlackString(pe.CreatedAt.Format("15:04:05")))
		fmt.Println()
	}

	fmt.Println("  /approve <n> or /approve all  - Send email(s)")
	fmt.Println("  /reject <n> or /reject all    - Discard email(s)")
}

func handleApprove(arg string) {
	if globalEmailOutbox == nil {
		fmt.Println(color.YellowString("Email not configured."))
		return
	}

	ctx := context.Background()

	if strings.ToLower(arg) == "all" {
		approved, err := globalEmailOutbox.ApproveAll(ctx)
		if err != nil {
			fmt.Println(color.RedString("Error approving emails: %v", err))
			return
		}
		if len(approved) == 0 {
			fmt.Println(color.YellowString("No pending emails to approve."))
			return
		}
		for _, pe := range approved {
			fmt.Println(color.GreenString("Sent %s to %s", pe.Type, pe.Recipients))
		}
		return
	}

	// Approve by index
	pending, err := globalEmailOutbox.GetPending(ctx)
	if err != nil {
		fmt.Println(color.RedString("Error loading outbox: %v", err))
		return
	}

	idx := 0
	if _, err := fmt.Sscanf(arg, "%d", &idx); err != nil || idx < 1 || idx > len(pending) {
		fmt.Println(color.RedString("Invalid index. Use /outbox to see pending emails."))
		return
	}

	pe := pending[idx-1]
	approved, err := globalEmailOutbox.Approve(ctx, pe.ID)
	if err != nil {
		fmt.Println(color.RedString("Error approving email: %v", err))
		return
	}
	fmt.Println(color.GreenString("Sent %s to %s", approved.Type, approved.Recipients))
}

func handleReject(arg string) {
	if globalEmailOutbox == nil {
		fmt.Println(color.YellowString("Email not configured."))
		return
	}

	ctx := context.Background()

	if strings.ToLower(arg) == "all" {
		rejected, err := globalEmailOutbox.RejectAll(ctx)
		if err != nil {
			fmt.Println(color.RedString("Error rejecting emails: %v", err))
			return
		}
		if len(rejected) == 0 {
			fmt.Println(color.YellowString("No pending emails to reject."))
			return
		}
		fmt.Println(color.YellowString("Rejected %d email(s).", len(rejected)))
		return
	}

	// Reject by index
	pending, err := globalEmailOutbox.GetPending(ctx)
	if err != nil {
		fmt.Println(color.RedString("Error loading outbox: %v", err))
		return
	}

	idx := 0
	if _, err := fmt.Sscanf(arg, "%d", &idx); err != nil || idx < 1 || idx > len(pending) {
		fmt.Println(color.RedString("Invalid index. Use /outbox to see pending emails."))
		return
	}

	pe := pending[idx-1]
	_, err = globalEmailOutbox.Reject(ctx, pe.ID)
	if err != nil {
		fmt.Println(color.RedString("Error rejecting email: %v", err))
		return
	}
	fmt.Println(color.YellowString("Rejected email to %s", pe.Recipients))
}
