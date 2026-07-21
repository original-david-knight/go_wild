package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
	"github.com/original-david-knight/go_wild/data"
	_ "github.com/original-david-knight/go_wild/knowledge_graph"
)

const defaultDatabaseURL = "postgres://gowild_agent:gowild_agent@localhost:5432/gowild_agent"

// openAdminDB opens a database connection for CLI admin commands (create-agent, delete-agent, etc.)
// that run on the host with direct database access. Caller must defer db.Close().
func openAdminDB() (gowild_data.Database, error) {
	db, err := openConfiguredDB()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return db, nil
}

// createNewAgent creates a new agent with its own wallet.
// Each agent gets a unique randomly-generated seed phrase.
// Use -seed-phrase to import an existing phrase instead.
// Use -telegram-token to set a Telegram bot token.
// Use -email-inbox to set an AgentMail inbox ID.
func createNewAgent(ctx context.Context, agentName string, seedPhrase string, telegramToken string, emailInbox string) error {
	// Validate agent name
	agentName = strings.ToLower(strings.TrimSpace(agentName))
	if agentName == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	// Generate new seed phrase if not provided
	if seedPhrase == "" {
		var err error
		seedPhrase, err = gowild_crypto.GenerateMnemonic()
		if err != nil {
			return fmt.Errorf("failed to generate seed phrase: %w", err)
		}
		fmt.Println(color.YellowString("Generated new wallet seed phrase:"))
		fmt.Println()
		fmt.Println(color.CyanString("  %s", seedPhrase))
		fmt.Println()
		fmt.Println(color.YellowString("⚠️  SAVE THIS PHRASE! It cannot be recovered."))
		fmt.Println()
	} else {
		// Validate provided seed phrase
		if !gowild_crypto.ValidateMnemonic(seedPhrase) {
			return fmt.Errorf("invalid seed phrase")
		}
	}

	// Initialize database
	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Check if agent already exists
	service := data.NewAgentService(db, agentName)
	existing, _ := service.GetAgent(ctx)
	if existing != nil {
		return fmt.Errorf("agent '%s' already exists", agentName)
	}

	// Derive keys at index 0 (each agent has its own seed phrase)
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return fmt.Errorf("failed to derive keys: %w", err)
	}

	// Create the agent
	if _, err := service.EnsureAgent(ctx); err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	// Set the seed phrase (derivation index is always 0)
	if err := service.SetWalletSeedPhrase(ctx, seedPhrase); err != nil {
		return fmt.Errorf("failed to set wallet config: %w", err)
	}

	// Set Telegram token if provided
	if telegramToken != "" {
		if err := service.SetTelegramBotToken(ctx, telegramToken); err != nil {
			return fmt.Errorf("failed to set telegram token: %w", err)
		}
	}

	// Set email API key from env or flag if available
	emailAPIKey := *emailAPIKeyFlag
	if emailAPIKey == "" {
		emailAPIKey = os.Getenv("AGENTMAIL_API_KEY")
	}
	if emailAPIKey != "" {
		if err := service.SetAgentMailAPIKey(ctx, emailAPIKey); err != nil {
			return fmt.Errorf("failed to set email API key: %w", err)
		}
	}

	// Set email inbox if provided
	if emailInbox != "" {
		if err := service.SetAgentMailInboxID(ctx, emailInbox); err != nil {
			return fmt.Errorf("failed to set email inbox: %w", err)
		}
	}

	// Display the new agent info
	fmt.Println(color.GreenString("Created agent: %s", agentName))
	fmt.Println()
	fmt.Println(color.HiBlackString("Ethereum address: %s", derived.EthAddress))
	fmt.Println(color.HiBlackString("Solana address:   %s", derived.SolAddress))
	if telegramToken != "" {
		fmt.Println(color.HiBlackString("Telegram:         configured"))
	}
	if emailInbox != "" {
		fmt.Println(color.HiBlackString("Email inbox:      %s", emailInbox))
	}
	fmt.Println()
	fmt.Println("To run as this agent:")
	fmt.Println(color.CyanString("  ./agent -agent %s", agentName))
	fmt.Println("Or:")
	fmt.Println(color.CyanString("  GOWILD_AGENT_ID=%s ./agent", agentName))

	return nil
}

// setTelegramToken sets the Telegram bot token for an existing agent.
func setTelegramToken(ctx context.Context, agentID string, token string) error {
	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	service := data.NewAgentService(db, agentID)

	// Verify agent exists
	agent, err := service.GetAgent(ctx)
	if err != nil {
		return fmt.Errorf("agent '%s' not found", agentID)
	}

	// Set the token
	if err := service.SetTelegramBotToken(ctx, token); err != nil {
		return fmt.Errorf("failed to set telegram token: %w", err)
	}

	fmt.Println(color.GreenString("Telegram bot token set for agent: %s", agent.Name))
	fmt.Println()
	fmt.Println("To verify, run:")
	fmt.Println(color.CyanString("  ./agent -agent %s", agentID))
	fmt.Println()
	fmt.Println("The agent will now have access to Telegram tools.")

	return nil
}

// setEmailAPIKey sets the AgentMail API key for an existing agent.
func setEmailAPIKey(ctx context.Context, agentID string, apiKey string) error {
	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	service := data.NewAgentService(db, agentID)

	// Verify agent exists
	agent, err := service.GetAgent(ctx)
	if err != nil {
		return fmt.Errorf("agent '%s' not found", agentID)
	}

	if err := service.SetAgentMailAPIKey(ctx, apiKey); err != nil {
		return fmt.Errorf("failed to set email API key: %w", err)
	}

	fmt.Println(color.GreenString("Email API key set for agent: %s", agent.Name))
	return nil
}

// setEmailInbox sets the AgentMail inbox ID for an existing agent.
func setEmailInbox(ctx context.Context, agentID string, inboxID string) error {
	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	service := data.NewAgentService(db, agentID)

	// Verify agent exists
	agent, err := service.GetAgent(ctx)
	if err != nil {
		return fmt.Errorf("agent '%s' not found", agentID)
	}

	// Set the inbox ID
	if err := service.SetAgentMailInboxID(ctx, inboxID); err != nil {
		return fmt.Errorf("failed to set email inbox: %w", err)
	}

	fmt.Println(color.GreenString("Email inbox set for agent: %s", agent.Name))
	fmt.Println(color.HiBlackString("Inbox ID: %s", inboxID))
	fmt.Println()
	fmt.Println("To verify, run:")
	fmt.Println(color.CyanString("  ./agent -agent %s", agentID))
	fmt.Println()
	fmt.Println("The agent will now have access to email tools.")

	return nil
}

// setTelegramTokenInteractive updates the local agent config when direct
// database access is available.
func setTelegramTokenInteractive(ctx context.Context, token string, agent *loop.AgenticLoop) error {
	if globalAgentService == nil {
		return fmt.Errorf("telegram configuration requires direct database access")
	}
	if err := globalAgentService.SetTelegramBotToken(ctx, token); err != nil {
		return fmt.Errorf("failed to set telegram token: %w", err)
	}
	if agent != nil {
		addLocalTelegramTools(ctx, agent, globalAgentService)
	}
	return nil
}

// setEmailAPIKeyInteractive updates the local agent config when direct
// database access is available.
func setEmailAPIKeyInteractive(ctx context.Context, apiKey string, agent *loop.AgenticLoop) error {
	if globalAgentService == nil {
		return fmt.Errorf("email configuration requires direct database access")
	}
	if err := globalAgentService.SetAgentMailAPIKey(ctx, apiKey); err != nil {
		return fmt.Errorf("failed to set email API key: %w", err)
	}
	if agent != nil {
		addLocalEmailTools(ctx, agent, globalAgentService)
	}
	return nil
}

// setEmailInboxInteractive updates the local agent config when direct
// database access is available.
func setEmailInboxInteractive(ctx context.Context, inboxID string, agent *loop.AgenticLoop) error {
	if globalAgentService == nil {
		return fmt.Errorf("email configuration requires direct database access")
	}
	if err := globalAgentService.SetAgentMailInboxID(ctx, inboxID); err != nil {
		return fmt.Errorf("failed to set email inbox: %w", err)
	}
	if agent != nil {
		addLocalEmailTools(ctx, agent, globalAgentService)
	}
	return nil
}

// listAgents lists all agents in the database.
func listAgents(ctx context.Context) error {
	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	agents, err := data.ListAgents(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	fmt.Println(color.CyanString("Agents:"))
	for _, agent := range agents {
		hasSoul := "no soul"
		if agent.HasSoul {
			hasSoul = "has soul"
		}
		fmt.Printf("  %s (%s, %s)\n", agent.ID, agent.Name, hasSoul)
	}
	return nil
}

// deleteAgent deletes an agent and all its associated data.
func deleteAgent(ctx context.Context, agentID string) error {
	agentID = strings.ToLower(strings.TrimSpace(agentID))
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	db, err := openAdminDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Check if agent exists
	service := data.NewAgentService(db, agentID)
	existing, err := service.GetAgent(ctx)
	if err != nil || existing == nil {
		return fmt.Errorf("agent '%s' not found", agentID)
	}

	// Confirm deletion
	fmt.Printf("Delete agent '%s' and all its data? [y/N] ", agentID)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Delete all agent data
	if err := service.DeleteAgent(ctx); err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	fmt.Println(color.GreenString("Deleted agent: %s", agentID))
	return nil
}
