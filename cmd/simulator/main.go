package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"consensus-benchmark/consensus"
	"consensus-benchmark/consensus/dag"
	"consensus-benchmark/consensus/hybrid"
	"consensus-benchmark/consensus/pbft"
	"consensus-benchmark/consensus/pos"
	"consensus-benchmark/consensus/pow"
	"consensus-benchmark/internal/monitor"
	"consensus-benchmark/internal/traffic"
	"consensus-benchmark/internal/types"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags)

	var (
		quick          = flag.Bool("quick", false, "run quick test (15s)")
		genTPS         = flag.Int("tps", 20, "generator transactions per second")
		hybridNodes    = flag.Int("hybridNodes", 4, "hybrid node count")
		hybridMaxTx    = flag.Int("hybridMaxTx", 200, "hybrid max tx per vertex")
		hybridTipCount = flag.Int("hybridTipCount", 2, "hybrid tip count")
		hybridFrontier = flag.Int("hybridFrontierMax", 20, "hybrid snapshot frontier max")
		hybridCutMax   = flag.Int("hybridCutMax", 200, "hybrid snapshot cut max")
	)
	flag.Parse()

	fmt.Println("=== ТЕСТОВЫЙ СТЕНД 4 АЛГОРИТМОВ КОНСЕНСУСА ===")
	fmt.Println("Алгоритмы: PBFT, PoS, PoW (через Geth Docker), DAG")
	fmt.Println()

	// Проверяем доступность Geth
	fmt.Println("Проверка доступности PoW (Geth Docker)...")
	gethAdapter := pow.NewGethAdapter()
	if err := gethAdapter.Initialize(1, nil); err != nil {
		fmt.Printf("⚠️  Geth недоступен: %v\n", err)
		fmt.Println("Запустите Geth в Docker: cd docker && docker-compose up -d")
		fmt.Println("Или продолжайте без PoW...")
		fmt.Println()
	}

	// Создаем монитор
	monitor := monitor.NewMonitor("results")

	// Создаем системы консенсуса
	systems := []struct {
		name    string
		system  consensus.ConsensusInterface
		enabled bool
	}{
		{
			name:    "Hybrid",
			system:  hybrid.NewHybrid(),
			enabled: true,
		},
		{
			name:    "PBFT",
			system:  pbft.NewPBFT(),
			enabled: true,
		},
		{
			name:    "PoS",
			system:  pos.NewPoS(),
			enabled: true,
		},
		{
			name:    "PoW",
			system:  gethAdapter,
			enabled: true,
		},
		{
			name:    "DAG",
			system:  dag.NewDAG(),
			enabled: true,
		},
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Канал для транзакций
	txChan := make(chan types.Transaction, 10000)

	// Запускаем генератор транзакций (меньше TPS для PoW)
	generator := traffic.NewGenerator(*genTPS) // по умолчанию 20 TPS чтобы не перегрузить PoW
	generator.Start(txChan)

	// sync.Map для thread-safe хранения запущенных систем
	runningSystems := sync.Map{}

	// Запускаем каждый алгоритм консенсуса
	for _, sys := range systems {
		if !sys.enabled {
			continue
		}

		wg.Add(1)

		go func(sysName string, system consensus.ConsensusInterface) {
			defer wg.Done()

			log.Printf("Инициализация: %s", sysName)

			// Конфигурация для каждого алгоритма
			var config map[string]interface{}
			var nodeCount int

			switch sysName {
			case "Hybrid":
				config = map[string]interface{}{
					"min_delay_ms":      10,
					"max_delay_ms":      50,
					"drop_prob":         0.0,
					"build_every_ms":    80,
					"snap_every_ms":     300,
					"seed":              42,
					"tip_count":         *hybridTipCount,
					"max_tx_per_vertex": *hybridMaxTx,
					"frontier_max":      *hybridFrontier,
					"cut_max":           *hybridCutMax,
				}
				nodeCount = *hybridNodes
			case "PBFT":
				config = map[string]interface{}{
					"faulty_nodes": 1,
				}
				nodeCount = 4
			case "PoS":
				config = map[string]interface{}{
					"block_time": 3 * time.Second,
				}
				nodeCount = 10
			case "PoW":
				config = map[string]interface{}{}
				nodeCount = 1
			case "DAG":
				config = map[string]interface{}{
					"target_tips": 2,
				}
				nodeCount = 20
			}

			// Инициализация
			if err := system.Initialize(nodeCount, config); err != nil {
				log.Printf("Ошибка инициализации %s: %v", sysName, err)
				return
			}

			// Запуск
			if err := system.Start(ctx); err != nil {
				log.Printf("Ошибка запуска %s: %v", sysName, err)
				return
			}

			monitor.AddSystem(system)
			runningSystems.Store(sysName, system)

			// Обработка транзакций
			go func(system consensus.ConsensusInterface, name string) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Восстановление после паники в %s: %v", name, r)
					}
				}()

				for {
					select {
					case <-ctx.Done():
						return
					case tx, ok := <-txChan:
						if !ok {
							return
						}

						// ИСПРАВЛЕНИЕ: Используем таймер вместо time.Sleep в select
						var delay time.Duration
						switch name {
						case "PBFT":
							delay = 5 * time.Millisecond
						case "PoS":
							delay = 10 * time.Millisecond
						case "PoW":
							delay = 100 * time.Millisecond
						case "DAG":
							delay = 2 * time.Millisecond
						}

						// Используем таймер с возможностью отмены
						timer := time.NewTimer(delay)
						select {
						case <-ctx.Done():
							timer.Stop()
							return
						case <-timer.C:
							// Продолжаем выполнение
						}

						// Отправляем транзакцию (добавляем таймаут)
						sendCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
						defer cancel()

						done := make(chan struct{})
						go func() {
							if _, err := system.SendTransaction(tx); err != nil {
								// Не логируем каждую ошибку для PoW (может быть недоступен)
								if name != "PoW" || !isConnectionError(err) {
									log.Printf("%s ошибка отправки: %v", name, err)
								}
							}
							close(done)
						}()

						select {
						case <-sendCtx.Done():
							// Таймаут или отмена
						case <-done:
							// Успешно
						}
					}
				}
			}(system, sysName)

			log.Printf("%s запущен успешно", sysName)

			// Ждем завершения контекста
			<-ctx.Done()

			// Останавливаем систему
			log.Printf("Остановка: %s...", sysName)
			system.Stop()
			log.Printf("%s остановлен", sysName)
		}(sys.name, sys.system)
	}

	// Ждем инициализации всех систем
	time.Sleep(5 * time.Second)

	// Запускаем монитор
	monitor.Start()

	// Ожидание сигнала завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	fmt.Println("\n✅ Стенд запущен. Нажмите Ctrl+C для остановки...")
	fmt.Println("📊 Собираем метрики...")
	fmt.Println()

	// Таймер для теста
	testDuration := 120 * time.Second
	if *quick {
		testDuration = 15 * time.Second
	}

	fmt.Printf("⏱️  Тест будет выполняться %v\n", testDuration)

	testTimer := time.NewTimer(testDuration)
	defer testTimer.Stop()

	select {
	case <-sigChan:
		fmt.Println("\n🛑 Получен сигнал остановки...")
	case <-testTimer.C:
		fmt.Println("\n✅ Тест завершен по таймауту...")
	}

	// Останавливаем все
	fmt.Println("\n🔄 Останавливаем системы...")
	cancel() // 1. Сначала отменяем контекст

	// Ждем немного, чтобы горутины получили сигнал
	time.Sleep(100 * time.Millisecond)

	// Сначала останавливаем генератор, чтобы он больше не писал в txChan.
	generator.Stop()

	// Затем закрываем канал транзакций для обработчиков.
	close(txChan)

	// Даем дополнительное время на завершение обработчиков
	time.Sleep(100 * time.Millisecond)

	// Останавливаем монитор
	monitor.Stop()

	// Ждем завершения всех горутин
	shutdownTimeout := 5 * time.Second // Уменьшаем таймаут
	shutdownChan := make(chan struct{})

	go func() {
		wg.Wait()
		close(shutdownChan)
	}()

	select {
	case <-shutdownChan:
		fmt.Println("✅ Все системы остановлены корректно")
	case <-time.After(shutdownTimeout):
		fmt.Println("⚠️  Таймаут остановки систем")
		fmt.Println("Завершаем принудительно...")
	}

	// Выводим статистику генератора
	sent, avgTPS := generator.GetStats()
	fmt.Printf("\n=== СТАТИСТИКА ГЕНЕРАТОРА ===\n")
	fmt.Printf("Всего отправлено транзакций: %d\n", sent)
	fmt.Printf("Средний TPS: %.2f\n", avgTPS)

	// Собираем все системы для анализа
	var allSystems []consensus.ConsensusInterface

	// Критический отладочный вывод
	fmt.Println("\n=== ОТЛАДОЧНАЯ ИНФОРМАЦИЯ О СИСТЕМАХ ===")

	runningSystems.Range(func(key, value interface{}) bool {
		sysName := key.(string)
		system := value.(consensus.ConsensusInterface)

		fmt.Printf("Система '%s' найдена в runningSystems\n", sysName)

		// Получаем метрики
		metrics := system.GetMetrics()
		fmt.Printf("  - TPS: %.2f, Блоки: %d, Транзакции: %d\n",
			metrics.AvgTPS, metrics.ConfirmedBlocks, metrics.TotalTransactions)

		allSystems = append(allSystems, system)
		return true
	})

	// Если систем нет в runningSystems, пробуем из исходного списка
	if len(allSystems) == 0 {
		fmt.Println("В runningSystems нет систем, пробуем исходный список...")
		for _, sys := range systems {
			if sys.enabled {
				fmt.Printf("Добавляем систему '%s' из исходного списка\n", sys.name)
				allSystems = append(allSystems, sys.system)
			}
		}
	}

	// Выводим РЕАЛЬНЫЙ анализ на основе измерений
	if len(allSystems) > 0 {
		fmt.Printf("\nНайдено %d систем для анализа\n", len(allSystems))
		printRealAnalysis(allSystems)
	} else {
		fmt.Println("❌ Нет данных для анализа")
	}

	fmt.Println("\n📁 Метрики сохранены в директории 'results/'")
	fmt.Println("1. 📄 summary_report.md - сводный отчет")
	fmt.Println("2. 📊 *.json - детальные метрики")
	fmt.Println("3. 📈 *.csv - метрики для анализа")
}

func isConnectionError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "connect") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "unreachable")
}

func printRealAnalysis(systems []consensus.ConsensusInterface) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 РЕАЛЬНЫЙ АНАЛИЗ РЕЗУЛЬТАТОВ НА ОСНОВЕ ИЗМЕРЕНИЙ")
	fmt.Println(strings.Repeat("=", 80))

	if len(systems) == 0 {
		fmt.Println("❌ Нет данных для анализа")
		return
	}

	// Отладочная информация
	fmt.Printf("Анализируем %d систем:\n", len(systems))
	for i, sys := range systems {
		fmt.Printf("%d. %s\n", i+1, sys.Name())
	}

	type Result struct {
		Name    string
		TPS     float64
		Energy  float64
		Latency float64
		Nodes   int
		Blocks  int64
		Txs     int64
		Success float64
		CPU     float64
		Memory  float64
	}

	var results []Result

	// Собираем реальные метрики
	for _, sys := range systems {
		metrics := sys.GetMetrics()

		// Отладочный вывод
		fmt.Printf("\nМетрики для %s:\n", sys.Name())
		fmt.Printf("  - TPS: %.2f\n", metrics.AvgTPS)
		fmt.Printf("  - Блоки: %d\n", metrics.ConfirmedBlocks)
		fmt.Printf("  - Транзакции: %d\n", metrics.TotalTransactions)
		fmt.Printf("  - Энергия: %.1f\n", metrics.EnergyConsumption)
		fmt.Printf("  - Задержка: %.2f мс\n", metrics.AvgLatencyMs)

		results = append(results, Result{
			Name:    sys.Name(),
			TPS:     metrics.AvgTPS,
			Energy:  metrics.EnergyConsumption,
			Latency: metrics.AvgLatencyMs,
			Nodes:   metrics.NodeCount,
			Blocks:  metrics.ConfirmedBlocks,
			Txs:     metrics.TotalTransactions,
			Success: metrics.SuccessRate,
			CPU:     metrics.CPUUsagePercent,
			Memory:  metrics.MemoryUsageMB,
		})
	}

	// 1. Сравнение по пропускной способности
	fmt.Println("\n" + strings.Repeat("-", 80))
	fmt.Println("1. 🚀 СРАВНЕНИЕ ПО ПРОПУСКНОЙ СПОСОБНОСТИ (TPS):")
	sort.Slice(results, func(i, j int) bool {
		return results[i].TPS > results[j].TPS
	})
	for i, r := range results {
		emoji := "📈"
		if i == 0 {
			emoji = "🏆"
		}
		fmt.Printf("   %s %-6s: %6.2f TPS (транзакций: %d)\n",
			emoji, r.Name, r.TPS, r.Txs)
	}

	// 2. Сравнение по энергоэффективности
	fmt.Println("\n2. 🔋 СРАВНЕНИЕ ПО ЭНЕРГОЭФФЕКТИВНОСТИ:")
	sort.Slice(results, func(i, j int) bool {
		return results[i].Energy < results[j].Energy
	})
	for i, r := range results {
		emoji := "📉"
		if i == 0 {
			emoji = "🌱"
		}
		fmt.Printf("   %s %-6s: %6.1f усл. ед. энергии\n",
			emoji, r.Name, r.Energy)
	}

	// 3. Сравнение по задержке
	fmt.Println("\n3. ⚡ СРАВНЕНИЕ ПО ЗАДЕРЖКЕ (latency):")
	sort.Slice(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})
	for i, r := range results {
		emoji := "⚡"
		if i == 0 {
			emoji = "🚀"
		}
		fmt.Printf("   %s %-6s: %6.2f мс\n",
			emoji, r.Name, r.Latency)
	}

	// 4. Сравнение по производимым блокам
	fmt.Println("\n4. 🧱 СРАВНЕНИЕ ПО ПРОИЗВЕДЕННЫМ БЛОКАМ:")
	sort.Slice(results, func(i, j int) bool {
		return results[i].Blocks > results[j].Blocks
	})
	for i, r := range results {
		emoji := "🧱"
		if i == 0 {
			emoji = "⭐"
		}
		fmt.Printf("   %s %-6s: %d блоков\n",
			emoji, r.Name, r.Blocks)
	}

	// 5. Сравнение по успешности
	fmt.Println("\n5. ✅ СРАВНЕНИЕ ПО УСПЕШНОСТИ:")
	sort.Slice(results, func(i, j int) bool {
		return results[i].Success > results[j].Success
	})
	for i, r := range results {
		emoji := "✅"
		if i == 0 {
			emoji = "🎯"
		}
		fmt.Printf("   %s %-6s: %.1f%% успешных операций\n",
			emoji, r.Name, r.Success*100)
	}

	// Компромиссы и выводы
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📋 ДЕТАЛЬНЫЕ МЕТРИКИ КАЖДОГО АЛГОРИТМА:")
	fmt.Println(strings.Repeat("=", 80))

	for _, r := range results {
		fmt.Printf("\n🔷 %s:\n", r.Name)
		fmt.Printf("   • Пропускная способность: %6.2f TPS\n", r.TPS)
		fmt.Printf("   • Энергопотребление:     %6.1f усл. ед.\n", r.Energy)
		fmt.Printf("   • Задержка:              %6.2f мс\n", r.Latency)
		fmt.Printf("   • Узлы сети:             %d\n", r.Nodes)
		fmt.Printf("   • Обработано блоков:     %d\n", r.Blocks)
		fmt.Printf("   • Всего транзакций:      %d\n", r.Txs)
		fmt.Printf("   • Успешность:            %.1f%%\n", r.Success*100)
		fmt.Printf("   • Потребление CPU:       %.1f%%\n", r.CPU)
		fmt.Printf("   • Потребление памяти:    %.1f MB\n", r.Memory)
	}

	// Рекомендации на основе данных
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("💡 РЕКОМЕНДАЦИИ НА ОСНОВЕ РЕАЛЬНЫХ РЕЗУЛЬТАТОВ:")
	fmt.Println(strings.Repeat("=", 80))

	// Находим лучшие по каждому критерию
	var bestTPS, bestEnergy, bestLatency, bestSuccess Result
	for _, r := range results {
		if r.TPS > bestTPS.TPS {
			bestTPS = r
		}
		if r.Energy < bestEnergy.Energy || bestEnergy.Name == "" {
			bestEnergy = r
		}
		if r.Latency < bestLatency.Latency || bestLatency.Name == "" {
			bestLatency = r
		}
		if r.Success > bestSuccess.Success || bestSuccess.Name == "" {
			bestSuccess = r
		}
	}

	if bestTPS.Name != "" {
		fmt.Printf("• 🚀 Для максимальной скорости:        %s (%.2f TPS)\n",
			bestTPS.Name, bestTPS.TPS)
	}
	if bestEnergy.Name != "" {
		fmt.Printf("• 🌱 Для энергоэффективности:          %s (%.1f усл. ед.)\n",
			bestEnergy.Name, bestEnergy.Energy)
	}
	if bestLatency.Name != "" {
		fmt.Printf("• ⚡ Для минимальной задержки:         %s (%.2f мс)\n",
			bestLatency.Name, bestLatency.Latency)
	}
	if bestSuccess.Name != "" {
		fmt.Printf("• ✅ Для максимальной надежности:      %s (%.1f%% успеха)\n",
			bestSuccess.Name, bestSuccess.Success*100)
	}

	// Общие рекомендации
	fmt.Println("\n🎯 ОБЩИЕ РЕКОМЕНДАЦИИ:")
	fmt.Println("• Для публичных сетей (децентрализация): PoW или PoS")
	fmt.Println("• Для консорциумных сетей (скорость): PBFT")
	fmt.Println("• Для IoT/микроплатежей (масштабируемость): DAG")
	fmt.Println("• Для high-frequency trading (задержка): PBFT")
	fmt.Println("• Для зеленых проектов (энергия): PoS или DAG")
}
