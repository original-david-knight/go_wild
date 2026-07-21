package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Wallet transaction operations

// LogWalletTransaction logs a wallet transaction.
func (s *AgentService) LogWalletTransaction(ctx context.Context, tx *WalletTransaction) error {
	dao := s.db.Table(WalletTransaction{})
	tx.ID = newID()
	tx.AgentID = s.agentID
	tx.Timestamp = time.Now()
	return dao.Insert(ctx, tx)
}

// UpdateWalletTransactionStatus updates the status of a transaction by its hash.
func (s *AgentService) UpdateWalletTransactionStatus(ctx context.Context, txHash, status, errorMsg string) error {
	dao := s.db.Table(WalletTransaction{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID, "transaction_hash": txHash},
		Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("transaction not found: %s", txHash)
	}

	tx := results[0].(*WalletTransaction)
	tx.Status = status
	tx.Error = errorMsg
	return dao.Update(ctx, tx)
}

// GetWalletTransactions retrieves recent wallet transactions.
func (s *AgentService) GetWalletTransactions(ctx context.Context, limit int) ([]*WalletTransaction, error) {
	dao := s.db.Table(WalletTransaction{})
	if limit <= 0 {
		limit = 50
	}

	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID},
		OrderBy:   "timestamp",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	txs := make([]*WalletTransaction, len(results))
	for i, r := range results {
		txs[i] = r.(*WalletTransaction)
	}
	return txs, nil
}

// GetWalletTransactionByHash retrieves a transaction by its hash.
func (s *AgentService) GetWalletTransactionByHash(ctx context.Context, txHash string) (*WalletTransaction, error) {
	dao := s.db.Table(WalletTransaction{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID, "transaction_hash": txHash},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*WalletTransaction), nil
}
