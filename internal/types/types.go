package types

import "time"

// Transaction представляет простую транзакцию
type Transaction struct {
	ID        string    `json:"id"`
	Sender    string    `json:"sender"`
	Receiver  string    `json:"receiver"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
	Signature string    `json:"signature,omitempty"`
}

// Block представляет блок в блокчейне
type Block struct {
	Index        int           `json:"index"`
	Timestamp    time.Time     `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PreviousHash string        `json:"previous_hash"`
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce,omitempty"`
	Validator    string        `json:"validator,omitempty"`
}

// Metrics содержит все измеряемые параметры
type Metrics struct {
	Algorithm         string    `json:"algorithm"`
	NodeCount         int       `json:"node_count"`
	AvgTPS            float64   `json:"avg_tps"`
	AvgLatencyMs      float64   `json:"avg_latency_ms"`
	MaxLatencyMs      float64   `json:"max_latency_ms"`
	MinLatencyMs      float64   `json:"min_latency_ms"`
	CPUUsagePercent   float64   `json:"cpu_usage_percent"`
	MemoryUsageMB     float64   `json:"memory_usage_mb"`
	TotalMessages     int64     `json:"total_messages"`
	EnergyConsumption float64   `json:"energy_consumption"` // в условных единицах
	Throughput        float64   `json:"throughput"`
	SuccessRate       float64   `json:"success_rate"`
	Timestamp         time.Time `json:"timestamp"`
	TotalTransactions int64     `json:"total_transactions"`
	ConfirmedBlocks   int64     `json:"confirmed_blocks"`
	ForkCount         int       `json:"fork_count"`
	ConsensusTimeAvg  float64   `json:"consensus_time_avg"`
	NetworkUsageMB    float64   `json:"network_usage_mb"`
	Notes             string    `json:"notes"`
}
