package tools

import (
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
)

// NewWalletToolsWithConfig creates a new WalletTools instance with explicit wallet configuration.
func NewWalletToolsWithConfig(config gowild_crypto.WalletConfig) (*WalletTools, error) {
	wallet, err := gowild_crypto.NewWallet(config)
	if err != nil {
		return nil, err
	}
	return &WalletTools{wallet: wallet}, nil
}

// NewWalletToolsWithAgentService creates a new WalletTools instance with AgentService for logging.
// This uses PostgreSQL via gowild_data instead of SQLite.
func NewWalletToolsWithAgentService(ethPrivateKey, solPrivateKey string, agentService *data.AgentService) (*WalletTools, error) {
	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{
		EthPrivateKey: ethPrivateKey,
		SolPrivateKey: solPrivateKey,
	})
	if err != nil {
		return nil, err
	}

	return &WalletTools{
		wallet:       wallet,
		agentService: agentService,
	}, nil
}
