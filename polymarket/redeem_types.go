package gowild_polymarket

import (
	"math/big"
	"regexp"
	"time"
)

const conditionalTokensABI = `[
	{
		"inputs": [
			{"internalType": "address", "name": "collateralToken", "type": "address"},
			{"internalType": "bytes32", "name": "parentCollectionId", "type": "bytes32"},
			{"internalType": "bytes32", "name": "conditionId", "type": "bytes32"},
			{"internalType": "uint256[]", "name": "indexSets", "type": "uint256[]"}
		],
			"name": "redeemPositions",
			"outputs": [],
			"stateMutability": "nonpayable",
			"type": "function"
		},
		{
			"inputs": [
				{"internalType": "bytes32", "name": "parentCollectionId", "type": "bytes32"},
				{"internalType": "bytes32", "name": "conditionId", "type": "bytes32"},
				{"internalType": "uint256", "name": "indexSet", "type": "uint256"}
			],
			"name": "getCollectionId",
			"outputs": [{"internalType": "bytes32", "name": "", "type": "bytes32"}],
			"stateMutability": "pure",
			"type": "function"
		},
		{
			"inputs": [
				{"internalType": "address", "name": "collateralToken", "type": "address"},
				{"internalType": "bytes32", "name": "collectionId", "type": "bytes32"}
			],
			"name": "getPositionId",
			"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
			"stateMutability": "pure",
			"type": "function"
		},
		{
			"inputs": [
				{"internalType": "address", "name": "account", "type": "address"},
				{"internalType": "uint256", "name": "id", "type": "uint256"}
			],
			"name": "balanceOf",
			"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
			"stateMutability": "view",
			"type": "function"
		},
		{
			"inputs": [{"internalType": "bytes32", "name": "conditionId", "type": "bytes32"}],
			"name": "payoutDenominator",
			"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
			"stateMutability": "view",
			"type": "function"
		},
		{
			"inputs": [
				{"internalType": "bytes32", "name": "conditionId", "type": "bytes32"},
				{"internalType": "uint256", "name": "", "type": "uint256"}
			],
			"name": "payoutNumerators",
			"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
			"stateMutability": "view",
			"type": "function"
		},
		{
			"anonymous": false,
			"inputs": [
				{"indexed": true, "internalType": "address", "name": "redeemer", "type": "address"},
				{"indexed": true, "internalType": "address", "name": "collateralToken", "type": "address"},
				{"indexed": true, "internalType": "bytes32", "name": "parentCollectionId", "type": "bytes32"},
				{"indexed": false, "internalType": "bytes32", "name": "conditionId", "type": "bytes32"},
				{"indexed": false, "internalType": "uint256[]", "name": "indexSets", "type": "uint256[]"},
				{"indexed": false, "internalType": "uint256", "name": "payout", "type": "uint256"}
			],
			"name": "PayoutRedemption",
			"type": "event"
		},
		{
			"inputs": [
				{"internalType": "address", "name": "owner", "type": "address"},
				{"internalType": "address", "name": "operator", "type": "address"}
			],
			"name": "isApprovedForAll",
			"outputs": [{"internalType": "bool", "name": "", "type": "bool"}],
			"stateMutability": "view",
			"type": "function"
		},
		{
			"inputs": [
				{"internalType": "address", "name": "operator", "type": "address"},
				{"internalType": "bool", "name": "approved", "type": "bool"}
			],
			"name": "setApprovalForAll",
			"outputs": [],
			"stateMutability": "nonpayable",
			"type": "function"
		}
]`

const negRiskAdapterABI = `[
	{
		"inputs": [
			{"internalType": "bytes32", "name": "_conditionId", "type": "bytes32"},
			{"internalType": "uint256[]", "name": "_amounts", "type": "uint256[]"}
		],
		"name": "redeemPositions",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"anonymous": false,
		"inputs": [
			{"indexed": true, "internalType": "address", "name": "redeemer", "type": "address"},
			{"indexed": true, "internalType": "bytes32", "name": "conditionId", "type": "bytes32"},
			{"indexed": false, "internalType": "uint256[]", "name": "amounts", "type": "uint256[]"},
			{"indexed": false, "internalType": "uint256", "name": "payout", "type": "uint256"}
		],
		"name": "PayoutRedemption",
		"type": "event"
	}
]`

const (
	redeemReceiptWaitTimeout = 2 * time.Minute
	redeemReceiptPollDelay   = 2 * time.Second
	redeemRPCRetryBaseDelay  = 2 * time.Second
	redeemRPCRetryMaxDelay   = 10 * time.Second
	redeemRPCRetryMaxRetries = 3
)

var retryAfterSecondsPattern = regexp.MustCompile(`(?i)retry in\s+(\d+)\s*s`)

type redeemTarget struct {
	conditionID string
	indexSets   []*big.Int
}

type negRiskRedeemTarget struct {
	conditionID string
	yesAmount   *big.Int
	noAmount    *big.Int
}

// RedeemWinningsTx describes one submitted redeem transaction.
type RedeemWinningsTx struct {
	ConditionID      string   `json:"condition_id"`
	IndexSets        []string `json:"index_sets"`
	TransactionHash  string   `json:"transaction_hash,omitempty"`
	ExplorerURL      string   `json:"explorer_url,omitempty"`
	CollateralPayout string   `json:"collateral_payout,omitempty"`
	ReceiptStatus    string   `json:"receipt_status"`
	Error            string   `json:"error,omitempty"`
}

// RedeemWinningsResult is the structured response from RedeemWinnings.
type RedeemWinningsResult struct {
	Address                string             `json:"address"`
	RPCURL                 string             `json:"rpc_url"`
	CollateralTokenAddress string             `json:"collateral_token_address"`
	ConditionsRedeemed     int                `json:"conditions_redeemed"`
	ConditionsFailed       int                `json:"conditions_failed,omitempty"`
	ConditionsSubmitted    int                `json:"conditions_submitted"`
	TotalCollateralPayout  string             `json:"total_collateral_payout"`
	ZeroPayoutConditions   []string           `json:"zero_payout_conditions,omitempty"`
	Transactions           []RedeemWinningsTx `json:"transactions"`
}
