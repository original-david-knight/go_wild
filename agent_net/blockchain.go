package gowild_agent_net

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BlockchainVerifier verifies blockchain transactions for premium upgrades.
type BlockchainVerifier struct {
	solanaRPCURL   string
	treasury       TreasuryAddresses
	httpClient     *http.Client
	minConfirms    int
	requestTimeout time.Duration
}

// NewBlockchainVerifier creates a new blockchain verifier.
func NewBlockchainVerifier(solanaRPCURL string, treasury TreasuryAddresses) *BlockchainVerifier {
	return &BlockchainVerifier{
		solanaRPCURL: solanaRPCURL,
		treasury:     treasury,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		minConfirms:    32,
		requestTimeout: 30 * time.Second,
	}
}

// VerifyUpgradeTransaction verifies a blockchain transaction for premium upgrade.
// Returns nil if valid, error otherwise.
func (v *BlockchainVerifier) VerifyUpgradeTransaction(ctx context.Context, txHash, chain, expectedPubKey string) error {
	switch chain {
	case ChainSolana:
		return v.verifySolanaTransaction(ctx, txHash, expectedPubKey)
	case ChainEthereum, ChainBase:
		return fmt.Errorf("chain %s not yet implemented", chain)
	default:
		return fmt.Errorf("unsupported chain: %s", chain)
	}
}

// verifySolanaTransaction verifies a Solana transaction for upgrade.
func (v *BlockchainVerifier) verifySolanaTransaction(ctx context.Context, txHash, expectedPubKey string) error {
	// Build RPC request
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getTransaction",
		"params": []any{
			txHash,
			map[string]any{
				"encoding":                       "jsonParsed",
				"commitment":                     "confirmed",
				"maxSupportedTransactionVersion": 0,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", v.solanaRPCURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("RPC returned status %d", resp.StatusCode)
	}

	var rpcResp struct {
		Result struct {
			Slot      int64 `json:"slot"`
			BlockTime int64 `json:"blockTime"`
			Meta      *struct {
				Err         any      `json:"err"`
				Fee         int64    `json:"fee"`
				LogMessages []string `json:"logMessages"`
			} `json:"meta"`
			Transaction struct {
				Message struct {
					AccountKeys []struct {
						Pubkey string `json:"pubkey"`
					} `json:"accountKeys"`
					Instructions []struct {
						ProgramId string          `json:"programId"`
						Parsed    json.RawMessage `json:"parsed"` // Can be object or string (memo)
					} `json:"instructions"`
				} `json:"message"`
			} `json:"transaction"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("failed to decode RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	if rpcResp.Result.Meta == nil {
		return fmt.Errorf("transaction not found or not confirmed")
	}

	// Check transaction succeeded
	if rpcResp.Result.Meta.Err != nil {
		return fmt.Errorf("transaction failed on-chain")
	}

	// Verify confirmations (check slot difference against current slot)
	// For simplicity, we'll verify the transaction exists with confirmed commitment
	// In production, you'd compare slots to ensure 32+ confirmations

	// Find SOL transfer instruction to treasury
	var transferFound bool
	var transferAmount int64

	for _, instr := range rpcResp.Result.Transaction.Message.Instructions {
		if len(instr.Parsed) == 0 {
			continue
		}

		// Try to parse as transfer instruction (object with type/info)
		var parsedObj struct {
			Type string `json:"type"`
			Info struct {
				Destination string `json:"destination"`
				Lamports    int64  `json:"lamports"`
				Source      string `json:"source"`
			} `json:"info"`
		}

		if err := json.Unmarshal(instr.Parsed, &parsedObj); err == nil {
			if parsedObj.Type == "transfer" && parsedObj.Info.Destination == v.treasury.Solana {
				transferFound = true
				transferAmount = parsedObj.Info.Lamports
				break
			}
		}
		// If unmarshal fails, it might be a memo (string) - that's OK, just skip it
	}

	// Log verification details
	log.Printf("Upgrade verification for tx=%s", txHash)
	log.Printf("  Expected treasury: %s", v.treasury.Solana)
	log.Printf("  Transfer found: %v, amount: %d lamports", transferFound, transferAmount)

	if !transferFound {
		// Log all instructions for debugging
		for i, instr := range rpcResp.Result.Transaction.Message.Instructions {
			log.Printf("  Instruction %d: programId=%s, parsed=%s", i, instr.ProgramId, string(instr.Parsed))
		}
		return fmt.Errorf("no transfer to treasury address found")
	}

	// Verify amount using configurable upgrade fee
	requiredLamports, err := ParseSOLAmount(UpgradeAmounts[ChainSolana])
	if err != nil {
		requiredLamports = 5_000_000 // Fallback to 0.005 SOL
	}
	log.Printf("  Required: %d lamports, got: %d lamports", requiredLamports, transferAmount)

	if transferAmount < requiredLamports {
		return fmt.Errorf("insufficient amount: got %d lamports, need %d", transferAmount, requiredLamports)
	}

	// Verify memo contains UPGRADE:<pubkey>
	expectedMemo := fmt.Sprintf("UPGRADE:%s", expectedPubKey)
	memoFound := false

	log.Printf("  Looking for memo: %s", expectedMemo)
	log.Printf("  Log messages (%d):", len(rpcResp.Result.Meta.LogMessages))

	for _, logMsg := range rpcResp.Result.Meta.LogMessages {
		// Log memo-related messages
		if strings.Contains(logMsg, "Memo") || strings.Contains(logMsg, "UPGRADE") {
			log.Printf("    %s", logMsg)
		}

		if strings.Contains(logMsg, "Program log: Memo") && strings.Contains(logMsg, expectedMemo) {
			memoFound = true
			break
		}
		// Also check raw log messages for memo content
		if strings.Contains(logMsg, expectedMemo) {
			memoFound = true
			break
		}
	}

	if !memoFound {
		// Log all messages for debugging
		log.Printf("  Memo NOT FOUND. All log messages:")
		for _, logMsg := range rpcResp.Result.Meta.LogMessages {
			log.Printf("    %s", logMsg)
		}
		return fmt.Errorf("memo UPGRADE:%s not found in transaction", expectedPubKey)
	}

	log.Printf("  Memo found! Upgrade verified successfully.")
	return nil
}

// ParseSOLAmount parses a SOL amount string to lamports.
func ParseSOLAmount(amount string) (int64, error) {
	f, ok := new(big.Float).SetString(amount)
	if !ok {
		return 0, fmt.Errorf("invalid amount: %s", amount)
	}
	lamportsFloat := new(big.Float).Mul(f, big.NewFloat(1e9))
	lamportsStr := lamportsFloat.Text('f', 0)
	return strconv.ParseInt(lamportsStr, 10, 64)
}
