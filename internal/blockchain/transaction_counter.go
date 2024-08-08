package blockchain

import (
	"log"
	"sync"

	"github.com/Soar-Robotics/SoarchainObserver/internal/blockchain/types"
)

type TransactionCounter struct {
	counts map[string]int
	mu     sync.Mutex
}

func NewTransactionCounter() *TransactionCounter {
	return &TransactionCounter{
		counts: make(map[string]int),
	}
}

func (tc *TransactionCounter) CountTransaction(tx *types.Transaction) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Determine the transaction type
	txType := tx.Type
	tc.counts[txType]++
}

func (tc *TransactionCounter) PrintCounts(logger *log.Logger) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	for txType, count := range tc.counts {
		logger.Printf("Transaction Type: %s, Count: %d", txType, count)
	}
}
