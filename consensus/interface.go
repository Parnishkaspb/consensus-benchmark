package consensus

import (
	"consensus-benchmark/internal/types"
	"context"
)

// ConsensusInterface - общий интерфейс для всех алгоритмов консенсуса
type ConsensusInterface interface {
	// Инициализация сети
	Initialize(nodes int, config map[string]interface{}) error

	// Запуск сети
	Start(ctx context.Context) error

	// Остановка сети
	Stop() error

	// Отправка транзакции
	SendTransaction(tx types.Transaction) (string, error)

	// Получение метрик
	GetMetrics() types.Metrics

	// Получение имени алгоритма
	Name() string

	// Проверка работоспособности
	IsHealthy() bool

	// Получение количества узлов
	NodeCount() int
}
