package hybrid

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Parnishkaspb/ConsensusHybrid/pkg/hybridbench"

	"consensus-benchmark/internal/energy"
	"consensus-benchmark/internal/types"
)

type Hybrid struct {
	mu      sync.RWMutex
	running bool

	runner    *hybridbench.Runner
	startTime time.Time

	// config
	nodeCount      int
	minDelay       time.Duration
	maxDelay       time.Duration
	dropProb       float64
	seed           int64
	buildEvery     time.Duration
	snapEvery      time.Duration
	tipCount       int
	maxTxPerVertex int
	frontierMax    int
	cutMax         int
}

func NewHybrid() *Hybrid {
	return &Hybrid{
		minDelay:       10 * time.Millisecond,
		maxDelay:       50 * time.Millisecond,
		dropProb:       0,
		seed:           42,
		buildEvery:     80 * time.Millisecond,
		snapEvery:      300 * time.Millisecond,
		tipCount:       2,
		maxTxPerVertex: 200,
		frontierMax:    20,
		cutMax:         200,
	}
}

func (h *Hybrid) Name() string { return "Hybrid" }

func (h *Hybrid) NodeCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.nodeCount
}

func (h *Hybrid) Initialize(nodes int, config map[string]interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nodeCount = nodes
	if nodes <= 0 {
		return fmt.Errorf("invalid nodes: %d", nodes)
	}

	// parse optional config
	if v, ok := getInt(config, "seed"); ok {
		h.seed = int64(v)
	}
	if v, ok := getInt(config, "min_delay_ms"); ok {
		h.minDelay = time.Duration(v) * time.Millisecond
	}
	if v, ok := getInt(config, "max_delay_ms"); ok {
		h.maxDelay = time.Duration(v) * time.Millisecond
	}
	if v, ok := getFloat(config, "drop_prob"); ok {
		h.dropProb = v
	}
	if v, ok := getInt(config, "build_every_ms"); ok {
		h.buildEvery = time.Duration(v) * time.Millisecond
	}
	if v, ok := getInt(config, "snap_every_ms"); ok {
		h.snapEvery = time.Duration(v) * time.Millisecond
	}
	if v, ok := getInt(config, "tip_count"); ok {
		h.tipCount = v
	}
	if v, ok := getInt(config, "max_tx_per_vertex"); ok {
		h.maxTxPerVertex = v
	}
	if v, ok := getInt(config, "frontier_max"); ok {
		h.frontierMax = v
	}
	if v, ok := getInt(config, "cut_max"); ok {
		h.cutMax = v
	}

	r, err := hybridbench.NewRunner(hybridbench.Config{
		Nodes:          nodes,
		Seed:           h.seed,
		MinDelay:       h.minDelay,
		MaxDelay:       h.maxDelay,
		DropProb:       h.dropProb,
		BuildEvery:     h.buildEvery,
		SnapEvery:      h.snapEvery,
		TipCount:       h.tipCount,
		MaxTxPerVertex: h.maxTxPerVertex,
		FrontierMax:    h.frontierMax,
		CutMax:         h.cutMax,
	})
	if err != nil {
		return err
	}
	h.runner = r
	h.running = false
	return nil
}

func (h *Hybrid) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return fmt.Errorf("hybrid already running")
	}
	if h.runner == nil {
		return fmt.Errorf("hybrid not initialized")
	}
	h.running = true
	h.startTime = time.Now()

	h.runner.Start()

	// stop on ctx cancel
	go func() {
		<-ctx.Done()
		_ = h.Stop()
	}()
	return nil
}

func (h *Hybrid) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running {
		return nil
	}
	h.running = false
	if h.runner != nil {
		h.runner.Stop()
	}
	return nil
}

func (h *Hybrid) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.running || h.runner == nil {
		return false
	}
	return h.runner.Metrics().FinalTx > 0
}

func (h *Hybrid) SendTransaction(tx types.Transaction) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.running || h.runner == nil {
		return "", fmt.Errorf("hybrid not running")
	}
	h.runner.SubmitBenchmarkTx(tx.ID, tx.Amount, tx.Timestamp)
	return tx.ID, nil
}

func (h *Hybrid) GetMetrics() types.Metrics {
	h.mu.RLock()
	defer h.mu.RUnlock()

	m := types.Metrics{
		Algorithm: "Hybrid",
		NodeCount: h.nodeCount,
		Timestamp: time.Now(),
	}
	if h.runner == nil {
		return m
	}

	rm := h.runner.Metrics()
	m.AvgTPS = rm.TPSFinal
	m.AvgLatencyMs = rm.AvgFinalLatencyMs
	m.MinLatencyMs = float64(rm.MinFinalLatencyMs)
	m.MaxLatencyMs = float64(rm.MaxFinalLatencyMs)
	m.TotalTransactions = int64(rm.SubmittedTx)
	m.ConfirmedBlocks = int64(rm.FinalCuts)
	m.SuccessRate = rm.FinalizedFrac

	// real-ish system metrics for parity with others
	m.CPUUsagePercent = rm.CPUUsagePercent
	m.MemoryUsageMB = rm.MemoryUsageMB
	m.TotalMessages = int64(rm.TotalMessages)
	m.NetworkUsageMB = rm.NetworkUsageMB
	m.EnergyConsumption = energy.Index(h.nodeCount, m.CPUUsagePercent, m.MemoryUsageMB, m.NetworkUsageMB)
	m.Throughput = m.AvgTPS

	return m
}

func getInt(m map[string]interface{}, k string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[k]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

func getFloat(m map[string]interface{}, k string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[k]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}
