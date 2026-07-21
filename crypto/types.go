package gowild_crypto

// Chain represents a supported blockchain network.
type Chain string

const (
	ChainEthereum Chain = "ethereum"
	ChainSolana   Chain = "solana"
)

// IsValid checks if the chain is supported.
func (c Chain) IsValid() bool {
	return c == ChainEthereum || c == ChainSolana
}

// WalletInfo contains public wallet information (no private keys).
type WalletInfo struct {
	Chain   Chain  `json:"chain"`
	Address string `json:"address"`
}

// SignedMessage contains a cryptographically signed message.
type SignedMessage struct {
	Chain     Chain  `json:"chain"`
	Address   string `json:"address"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

// TransactionResult contains the result of a blockchain transaction.
type TransactionResult struct {
	Chain           Chain  `json:"chain"`
	TransactionHash string `json:"transaction_hash"`
	FromAddress     string `json:"from_address"`
	ToAddress       string `json:"to_address"`
	Amount          string `json:"amount"`
	TokenAddress    string `json:"token_address,omitempty"`
	Status          string `json:"status"`
	ExplorerURL     string `json:"explorer_url,omitempty"`
	GasUsed         string `json:"gas_used,omitempty"`
}

// SwapResult contains the result of a token swap.
type SwapResult struct {
	Chain           Chain  `json:"chain"`
	TransactionHash string `json:"transaction_hash"`
	FromToken       string `json:"from_token"`
	ToToken         string `json:"to_token"`
	FromAmount      string `json:"from_amount"`
	ToAmount        string `json:"to_amount"`
	ExplorerURL     string `json:"explorer_url,omitempty"`
}

// ContractCallResult contains the result of a smart contract call.
type ContractCallResult struct {
	Chain           Chain  `json:"chain"`
	TransactionHash string `json:"transaction_hash,omitempty"`
	ContractAddress string `json:"contract_address"`
	Method          string `json:"method"`
	Result          any    `json:"result,omitempty"`
	ExplorerURL     string `json:"explorer_url,omitempty"`
}

// BalanceResult contains wallet balance information.
type BalanceResult struct {
	Chain        Chain  `json:"chain"`
	Address      string `json:"address"`
	Balance      string `json:"balance"`                 // Human-readable balance
	BalanceRaw   string `json:"balance_raw"`             // Raw balance in smallest units (wei/lamports)
	Symbol       string `json:"symbol"`                  // Token symbol (ETH, SOL, USDC, etc.)
	Decimals     int    `json:"decimals"`                // Token decimals
	TokenAddress string `json:"token_address,omitempty"` // Empty for native tokens
}
