package pbft

import (
	"context"
	"testing"
	"time"

	"consensus-benchmark/internal/types"
)

func TestPBFTCommitsTransactionsAndReportsSuccess(t *testing.T) {
	p := NewPBFT()
	if err := p.Initialize(4, nil); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer p.Stop()

	for i := 0; i < 5; i++ {
		_, err := p.SendTransaction(types.Transaction{
			ID:        string(rune('a' + i)),
			Sender:    "alice",
			Receiver:  "bob",
			Amount:    float64(i + 1),
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("SendTransaction() error = %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	var metrics types.Metrics
	for time.Now().Before(deadline) {
		metrics = p.GetMetrics()
		if metrics.ConfirmedBlocks > 0 && metrics.TotalTransactions > 0 && metrics.SuccessRate > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("PBFT did not commit transactions: blocks=%d tx=%d success=%f tps=%f",
		metrics.ConfirmedBlocks, metrics.TotalTransactions, metrics.SuccessRate, metrics.AvgTPS)
}
