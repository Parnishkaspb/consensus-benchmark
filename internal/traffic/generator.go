package traffic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"consensus-benchmark/internal/types"
)

type Generator struct {
	rate        int // транзакций в секунду
	running     bool
	stopChan    chan struct{}
	wg          sync.WaitGroup
	txSent      int64
	txConfirmed int64
	mu          sync.RWMutex
	startTime   time.Time
}

func NewGenerator(rate int) *Generator {
	return &Generator{
		rate:      rate,
		stopChan:  make(chan struct{}),
		startTime: time.Now(),
	}
}

func (g *Generator) Start(txChan chan<- types.Transaction) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return
	}

	g.running = true
	g.startTime = time.Now()

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.generateLoop(txChan)
	}()

	fmt.Printf("Генератор запущен с интенсивностью %d TPS\n", g.rate)
}

func (g *Generator) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return
	}

	close(g.stopChan)
	g.wg.Wait()
	g.running = false
	fmt.Println("Генератор остановлен")
}

func (g *Generator) generateLoop(txChan chan<- types.Transaction) {
	interval := time.Duration(float64(time.Second) / float64(g.rate))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopChan:
			return
		case <-ticker.C:
			tx := g.createTransaction()
			select {
			case txChan <- tx:
				g.mu.Lock()
				g.txSent++
				g.mu.Unlock()
			default:
				// Канал полон, пропускаем
			}
		}
	}
}

func (g *Generator) createTransaction() types.Transaction {
	g.mu.Lock()
	defer g.mu.Unlock()

	txID := fmt.Sprintf("tx_%d_%d", time.Now().UnixNano(), g.txSent)

	// Генерируем случайные адреса
	senders := []string{"alice", "bob", "charlie", "dave", "eve"}
	receivers := []string{"wallet1", "wallet2", "wallet3", "exchange", "miner"}

	sender := senders[rand.Intn(len(senders))]
	receiver := receivers[rand.Intn(len(receivers))]

	// Генерируем подпись
	data := fmt.Sprintf("%s:%s:%f:%d", sender, receiver, 1.0, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	signature := hex.EncodeToString(hash[:])

	return types.Transaction{
		ID:        txID,
		Sender:    sender,
		Receiver:  receiver,
		Amount:    0.1 + rand.Float64()*9.9,
		Timestamp: time.Now(),
		Signature: signature,
	}
}

func (g *Generator) GetStats() (sent int64, tps float64) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	duration := time.Since(g.startTime).Seconds()
	if duration > 0 {
		tps = float64(g.txSent) / duration
	}

	return g.txSent, tps
}
