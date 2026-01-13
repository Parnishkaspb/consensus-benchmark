package monitor

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"consensus-benchmark/consensus"
	"consensus-benchmark/internal/types"
)

type Monitor struct {
	consensusSystems   []consensus.ConsensusInterface
	metricsHistory     map[string][]types.Metrics
	outputDir          string
	running            bool
	stopChan           chan struct{}
	wg                 sync.WaitGroup
	mu                 sync.RWMutex
	collectionInterval time.Duration
}

func NewMonitor(outputDir string) *Monitor {
	if outputDir == "" {
		outputDir = "metrics"
	}

	// Создаем директорию для результатов
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Printf("Ошибка создания директории %s: %v", outputDir, err)
		outputDir = "."
	}

	return &Monitor{
		consensusSystems:   make([]consensus.ConsensusInterface, 0),
		metricsHistory:     make(map[string][]types.Metrics),
		outputDir:          outputDir,
		stopChan:           make(chan struct{}),
		collectionInterval: 2 * time.Second,
	}
}

func (m *Monitor) AddSystem(system consensus.ConsensusInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consensusSystems = append(m.consensusSystems, system)
	m.metricsHistory[system.Name()] = make([]types.Metrics, 0)
}

func (m *Monitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	// Канал мог быть закрыт при предыдущей остановке, создаем новый.
	m.stopChan = make(chan struct{})
	m.running = true
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()
		m.collectionLoop()
	}()

	log.Println("Монитор метрик запущен")
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}

	m.running = false
	close(m.stopChan)
	m.mu.Unlock()

	// Ждем завершения фонового сбора метрик вне блокировки,
	// иначе collectionLoop не сможет выйти, если держит этот mutex.
	m.wg.Wait()

	// Сохраняем финальные результаты
	m.saveAllMetrics()

	log.Println("Монитор метрик остановлен")
}

func (m *Monitor) collectionLoop() {
	ticker := time.NewTicker(m.collectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.collectMetrics()
		}
	}
}

func (m *Monitor) collectMetrics() {
	// Снимаем снимок списка систем без удержания мьютекса на время вызова GetMetrics.
	m.mu.RLock()
	systems := make([]consensus.ConsensusInterface, len(m.consensusSystems))
	copy(systems, m.consensusSystems)
	m.mu.RUnlock()

	for _, system := range systems {
		if system == nil {
			continue
		}

		name := system.Name()

		metricsCh := make(chan types.Metrics, 1)
		go func(sys consensus.ConsensusInterface) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Монитор: паника при получении метрик %s: %v", sys.Name(), r)
				}
			}()
			metricsCh <- sys.GetMetrics()
		}(system)

		select {
		case metrics := <-metricsCh:
			m.mu.Lock()
			m.metricsHistory[name] = append(m.metricsHistory[name], metrics)
			m.mu.Unlock()

			fmt.Printf("[%s] TPS: %.2f, Latency: %.2fms, Nodes: %d, Energy: %.1f\n",
				name, metrics.AvgTPS, metrics.AvgLatencyMs,
				metrics.NodeCount, metrics.EnergyConsumption)
		case <-time.After(2 * time.Second):
			log.Printf("Монитор: таймаут получения метрик от %s", name)
		}
	}
}

func (m *Monitor) saveAllMetrics() {
	// Сохраняем в JSON
	m.saveJSON()

	// Сохраняем в CSV
	m.saveCSV()

	// Генерируем сводный отчет
	m.generateSummaryReport()
}

func (m *Monitor) saveJSON() {
	for name, metrics := range m.metricsHistory {
		filename := filepath.Join(m.outputDir, fmt.Sprintf("%s_metrics.json", name))

		file, err := os.Create(filename)
		if err != nil {
			log.Printf("Ошибка создания файла %s: %v", filename, err)
			continue
		}
		defer file.Close()

		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(metrics); err != nil {
			log.Printf("Ошибка записи JSON для %s: %v", name, err)
		} else {
			log.Printf("Метрики %s сохранены в %s", name, filename)
		}
	}
}

func (m *Monitor) saveCSV() {
	for name, metrics := range m.metricsHistory {
		if len(metrics) == 0 {
			continue
		}

		filename := filepath.Join(m.outputDir, fmt.Sprintf("%s_metrics.csv", name))

		file, err := os.Create(filename)
		if err != nil {
			log.Printf("Ошибка создания файла %s: %v", filename, err)
			continue
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		// Записываем заголовок
		headers := []string{
			"timestamp", "algorithm", "node_count", "avg_tps", "avg_latency_ms",
			"cpu_usage_percent", "memory_usage_mb", "energy_consumption",
			"throughput", "success_rate", "total_transactions",
		}

		if err := writer.Write(headers); err != nil {
			log.Printf("Ошибка записи заголовка CSV для %s: %v", name, err)
			continue
		}

		// Записываем данные
		for _, metric := range metrics {
			record := []string{
				metric.Timestamp.Format(time.RFC3339),
				metric.Algorithm,
				fmt.Sprintf("%d", metric.NodeCount),
				fmt.Sprintf("%.2f", metric.AvgTPS),
				fmt.Sprintf("%.2f", metric.AvgLatencyMs),
				fmt.Sprintf("%.2f", metric.CPUUsagePercent),
				fmt.Sprintf("%.2f", metric.MemoryUsageMB),
				fmt.Sprintf("%.2f", metric.EnergyConsumption),
				fmt.Sprintf("%.2f", metric.Throughput),
				fmt.Sprintf("%.2f", metric.SuccessRate),
				fmt.Sprintf("%d", metric.TotalTransactions),
			}

			if err := writer.Write(record); err != nil {
				log.Printf("Ошибка записи записи CSV для %s: %v", name, err)
			}
		}

		log.Printf("CSV метрики %s сохранены в %s", name, filename)
	}
}

func (m *Monitor) generateSummaryReport() {
	filename := filepath.Join(m.outputDir, "summary_report.md")

	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Ошибка создания сводного отчета: %v", err)
		return
	}
	defer file.Close()

	// Генерируем Markdown отчет
	report := "# Сравнительный анализ алгоритмов консенсуса\n\n"
	report += "Тестовый стенд запущен: " + time.Now().Format("2006-01-02 15:04:05") + "\n\n"

	report += "## Критерии оценки:\n"
	report += "1. **Энергоэффективность** - потребление ресурсов на узел\n"
	report += "2. **Децентрализация** - количество узлов и их равноправие\n"
	report += "3. **Пропускная способность (TPS)** - транзакций в секунду\n"
	report += "4. **Безопасность** - устойчивость к атакам\n"
	report += "5. **Масштабируемость** - изменение производительности с ростом сети\n\n"

	report += "## Результаты тестирования:\n\n"

	for name, metrics := range m.metricsHistory {
		if len(metrics) == 0 {
			continue
		}

		// Берем последние метрики
		lastMetric := metrics[len(metrics)-1]

		report += fmt.Sprintf("### %s\n\n", name)
		report += fmt.Sprintf("- **Количество узлов:** %d\n", lastMetric.NodeCount)
		report += fmt.Sprintf("- **Средний TPS:** %.2f\n", lastMetric.AvgTPS)
		report += fmt.Sprintf("- **Средняя задержка:** %.2f мс\n", lastMetric.AvgLatencyMs)
		report += fmt.Sprintf("- **Потребление CPU:** %.1f%%\n", lastMetric.CPUUsagePercent)
		report += fmt.Sprintf("- **Потребление памяти:** %.1f MB\n", lastMetric.MemoryUsageMB)
		report += fmt.Sprintf("- **Энергопотребление:** %.1f усл. ед.\n", lastMetric.EnergyConsumption)
		report += fmt.Sprintf("- **Пропускная способность:** %.2f\n", lastMetric.Throughput)
		report += fmt.Sprintf("- **Успешность:** %.1f%%\n\n", lastMetric.SuccessRate*100)

		// Добавляем тезисы по критериям
		report += "**Тезисы:**\n"

		switch name {
		case "PBFT":
			report += "- Высокая пропускная способность в небольших доверенных сетях\n"
			report += "- Квадратичный рост сообщений при увеличении узлов\n"
			report += "- Низкое энергопотребление\n"
			report += "- Гарантированная финализация при f < N/3\n"
		case "PoS":
			report += "- Умеренное энергопотребление\n"
			report += "- Хорошая децентрализация при равномерном распределении стейка\n"
			report += "- Риск централизации капитала\n"
			report += "- Высокая пропускная способность\n"
		case "DAG":
			report += "- Очень низкое энергопотребление\n"
			report += "- Потенциально бесконечная масштабируемость\n"
			report += "- Высокая параллельность обработки транзакций\n"
			report += "- Сложность реализации и анализа безопасности\n"
		}

		report += "\n---\n\n"
	}

	// Добавляем сравнительную таблицу
	report += "## Сравнительная таблица\n\n"
	report += "| Алгоритм | TPS | Задержка | Энергоэфф. | Децентрал. | Безопасность |\n"
	report += "|----------|-----|----------|------------|------------|--------------|\n"

	for name, metrics := range m.metricsHistory {
		if len(metrics) == 0 {
			continue
		}

		lastMetric := metrics[len(metrics)-1]

		// Оценки по 5-балльной шкале
		var energyScore, decentralizationScore, securityScore string

		switch name {
		case "PBFT":
			energyScore = "★★★★★"
			decentralizationScore = "★★★☆☆"
			securityScore = "★★★★☆"
		case "PoS":
			energyScore = "★★★★☆"
			decentralizationScore = "★★★★☆"
			securityScore = "★★★☆☆"
		case "DAG":
			energyScore = "★★★★★"
			decentralizationScore = "★★★★★"
			securityScore = "★★★☆☆"
		}

		report += fmt.Sprintf("| %s | %.0f | %.0f мс | %s | %s | %s |\n",
			name, lastMetric.AvgTPS, lastMetric.AvgLatencyMs,
			energyScore, decentralizationScore, securityScore)
	}

	if _, err := file.WriteString(report); err != nil {
		log.Printf("Ошибка записи отчета: %v", err)
	} else {
		log.Printf("Сводный отчет сохранен в %s", filename)
	}
}

func (m *Monitor) GetMetricsHistory(name string) []types.Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.metricsHistory[name]
}
