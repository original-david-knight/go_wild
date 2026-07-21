package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
	"github.com/tyler-smith/go-bip39"
)

// AgentService provides operations for agent data.
type AgentService struct {
	db      gowild_data.Database
	agentID string
}

// NewAgentService creates a new AgentService.
func NewAgentService(db gowild_data.Database, agentID string) *AgentService {
	return &AgentService{
		db:      db,
		agentID: agentID,
	}
}

// newID generates a new UUID.
func newID() string {
	return uuid.New().String()
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// EnsureAgent ensures the agent record exists, creating it if necessary.
// New agents are automatically assigned a unique seed phrase for their wallet.
func (s *AgentService) EnsureAgent(ctx context.Context) (*Agent, error) {
	dao := s.db.Table(Agent{})

	var agent Agent
	err := dao.Get(ctx, s.agentID, &agent)
	if err == nil {
		// Agent exists - ensure it has a seed phrase
		if agent.WalletSeedPhrase == "" {
			seedPhrase, err := generateSeedPhrase()
			if err != nil {
				return nil, fmt.Errorf("failed to generate seed phrase: %w", err)
			}
			agent.WalletSeedPhrase = seedPhrase
			agent.UpdatedAt = time.Now()
			if err := dao.Update(ctx, &agent); err != nil {
				return nil, fmt.Errorf("failed to update agent with seed phrase: %w", err)
			}
		}
		return &agent, nil
	}

	// Agent doesn't exist, create it with a seed phrase
	seedPhrase, err := generateSeedPhrase()
	if err != nil {
		return nil, fmt.Errorf("failed to generate seed phrase: %w", err)
	}

	now := time.Now()
	agent = Agent{
		ID:               s.agentID,
		Name:             capitalizeFirst(s.agentID),
		WalletSeedPhrase: seedPhrase,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := dao.Insert(ctx, &agent); err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	return &agent, nil
}

// generateSeedPhrase generates a new BIP39 mnemonic seed phrase.
func generateSeedPhrase() (string, error) {
	entropy, err := bip39.NewEntropy(128) // 12 words
	if err != nil {
		return "", err
	}
	return bip39.NewMnemonic(entropy)
}

// GetAgent retrieves the agent record.
func (s *AgentService) GetAgent(ctx context.Context) (*Agent, error) {
	dao := s.db.Table(Agent{})
	var agent Agent
	if err := dao.Get(ctx, s.agentID, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// UpdateAgent updates the agent record.
func (s *AgentService) UpdateAgent(ctx context.Context, agent *Agent) error {
	agent.UpdatedAt = time.Now()
	return s.db.Table(Agent{}).Update(ctx, agent)
}

// GetWalletSeedPhrase retrieves the agent's wallet seed phrase.
// Returns empty string if no seed phrase is configured.
func (s *AgentService) GetWalletSeedPhrase(ctx context.Context) (string, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return "", err
	}
	return agent.WalletSeedPhrase, nil
}

// GetCompanyWalletSeedPhrase returns the company's wallet seed phrase if
// this agent belongs to a company. Returns an error if the agent is not in
// a company or the company has no seed phrase.
func (s *AgentService) GetCompanyWalletSeedPhrase(ctx context.Context) (string, error) {
	member, err := GetCompanyMemberForAgent(ctx, s.db, s.agentID)
	if err != nil {
		return "", fmt.Errorf("failed to check company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return "", fmt.Errorf("agent is not in a company")
	}
	return EnsureCompanyWalletSeedPhrase(ctx, s.db, member.CompanyID)
}

// SetWalletSeedPhrase sets the agent's wallet seed phrase.
func (s *AgentService) SetWalletSeedPhrase(ctx context.Context, seedPhrase string) error {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return err
	}
	agent.WalletSeedPhrase = seedPhrase
	return s.UpdateAgent(ctx, agent)
}

// GetTelegramBotToken retrieves the agent's Telegram bot token.
// Returns empty string if no token is configured.
func (s *AgentService) GetTelegramBotToken(ctx context.Context) (string, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return "", err
	}
	return agent.TelegramBotToken, nil
}

// SetTelegramBotToken sets the agent's Telegram bot token.
func (s *AgentService) SetTelegramBotToken(ctx context.Context, token string) error {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return err
	}
	agent.TelegramBotToken = token
	return s.UpdateAgent(ctx, agent)
}

// GetAgentMailAPIKey retrieves the agent's AgentMail API key.
// Returns empty string if no key is configured.
func (s *AgentService) GetAgentMailAPIKey(ctx context.Context) (string, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return "", err
	}
	return agent.AgentMailAPIKey, nil
}

// SetAgentMailAPIKey sets the agent's AgentMail API key.
func (s *AgentService) SetAgentMailAPIKey(ctx context.Context, apiKey string) error {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return err
	}
	agent.AgentMailAPIKey = apiKey
	return s.UpdateAgent(ctx, agent)
}

// GetAgentMailInboxID retrieves the agent's AgentMail inbox ID.
// Returns empty string if no inbox is configured.
func (s *AgentService) GetAgentMailInboxID(ctx context.Context) (string, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return "", err
	}
	return agent.AgentMailInboxID, nil
}

// SetAgentMailInboxID sets the agent's AgentMail inbox ID.
func (s *AgentService) SetAgentMailInboxID(ctx context.Context, inboxID string) error {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return err
	}
	agent.AgentMailInboxID = inboxID
	return s.UpdateAgent(ctx, agent)
}
