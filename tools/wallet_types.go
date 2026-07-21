package tools

import (
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
)

// GetWalletAddressInput defines the input for getting a wallet address.
type GetWalletAddressInput struct {
	Chain string `json:"chain" description:"The blockchain to get address for" required:"true" enum:"ethereum,solana"`
}

// SignMessageInput defines the input for signing a message.
type SignMessageInput struct {
	Chain   string `json:"chain" description:"The blockchain to sign with" required:"true" enum:"ethereum,solana"`
	Message string `json:"message" description:"The message to cryptographically sign" required:"true"`
}

// SendTokenInput defines the input for sending tokens.
type SendTokenInput struct {
	Chain        string `json:"chain" description:"The blockchain to use" required:"true" enum:"ethereum,solana"`
	To           string `json:"to" description:"Destination wallet address" required:"true"`
	Amount       string `json:"amount" description:"Amount to send in human units (e.g., '1.5' for 1.5 ETH/SOL)" required:"true"`
	TokenAddress string `json:"token_address,omitempty" description:"Token contract address for ERC20/SPL tokens. Leave empty for native ETH/SOL."`
	Memo         string `json:"memo,omitempty" description:"Optional memo to attach to the transaction (Solana only). Used for payment references like 'UPGRADE:pubkey'."`
}

// SwapTokenInput defines the input for swapping tokens.
type SwapTokenInput struct {
	Chain       string `json:"chain" description:"The blockchain to use" required:"true" enum:"ethereum,solana"`
	FromToken   string `json:"from_token" description:"Token to sell - 'ETH'/'SOL' for native, or contract address" required:"true"`
	ToToken     string `json:"to_token" description:"Token to buy - 'ETH'/'SOL' for native, or contract address" required:"true"`
	Amount      string `json:"amount" description:"Amount of from_token to swap in human units" required:"true"`
	SlippageBps int    `json:"slippage_bps,omitempty" description:"Max slippage in basis points (e.g., 50 = 0.5%). Default: 50"`
}

// ContractCallInput defines the input for calling a smart contract.
type ContractCallInput struct {
	Chain           string `json:"chain" description:"The blockchain to use" required:"true" enum:"ethereum,solana"`
	ContractAddress string `json:"contract_address" description:"Contract/program address" required:"true"`
	Method          string `json:"method" description:"Method signature for ETH (e.g., 'transfer(address,uint256)') or instruction name for Solana" required:"true"`
	Args            []any  `json:"args,omitempty" description:"Method arguments as array"`
	Value           string `json:"value,omitempty" description:"ETH value to send with the call (in ETH, e.g., '0.1')"`
	ReadOnly        bool   `json:"read_only,omitempty" description:"If true, only read state without sending transaction"`
}

// GetBalanceInput defines the input for getting wallet balance.
type GetBalanceInput struct {
	Chain        string `json:"chain" description:"The blockchain to check balance on" required:"true" enum:"ethereum,polygon,solana"`
	TokenAddress string `json:"token_address,omitempty" description:"Token contract address for ERC20/SPL tokens. Leave empty for native gas token balance (ETH/POL/SOL). For Polygon USDT.e use: 0xC2132D05D31c914a87C6611C10748AEb04B58e8F. For Polymarket collateral USDC on Polygon use: 0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174."`
}

// GetBalancesInput defines the input for getting a fixed, simplified set of balances.
// This intentionally has no fields so agents cannot pick unsupported combinations.
type GetBalancesInput struct{}

// GetTransactionHistoryInput defines the input for getting transaction history.
type GetTransactionHistoryInput struct {
	Limit int    `json:"limit,omitempty" description:"Maximum number of transactions to return (default 20, max 100)"`
	Chain string `json:"chain,omitempty" description:"Filter by chain (ethereum or solana). Leave empty for all chains." enum:"ethereum,solana"`
}

// EncryptMessageInput defines the input for encrypting a message with NaCl box.
type EncryptMessageInput struct {
	Plaintext          string `json:"plaintext" description:"The message text to encrypt" required:"true"`
	RecipientPublicKey string `json:"recipient_public_key" description:"Recipient's Ed25519 public key in base64url (no padding, 43 chars)" required:"true"`
}

// DecryptMessageInput defines the input for decrypting a NaCl box message.
type DecryptMessageInput struct {
	Ciphertext      string `json:"ciphertext" description:"Base64url-encoded ciphertext to decrypt" required:"true"`
	Nonce           string `json:"nonce" description:"Base64url-encoded 24-byte nonce" required:"true"`
	SenderPublicKey string `json:"sender_public_key" description:"Sender's Ed25519 public key in base64url (no padding, 43 chars)" required:"true"`
}

// GetEd25519PublicKeyInput defines the input for getting the agent's Ed25519 public key.
type GetEd25519PublicKeyInput struct{}

// WalletTools provides blockchain wallet tools.
// Private keys are stored per-agent in the database and never exposed.
type WalletTools struct {
	wallet       *gowild_crypto.Wallet
	agentService *data.AgentService // For transaction logging (optional)
}
