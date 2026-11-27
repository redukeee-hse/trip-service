package config

import "time"

// PositionsConfig — опрос устаревших поездок (ЛР3 п. 5.3–5.4).
type PositionsConfig struct {
	CheckInterval time.Duration // POSITION_CHECK_INTERVAL: период тикера сканера = пауза между повторными пушами
	StaleAfter    time.Duration // POSITION_STALE_AFTER: сколько молчания считается «давно нет координат»
	Workers       int           // POSITION_WORKERS: сколько воркеров живёт в пуле
	QueueSize     int           // POSITION_QUEUE_SIZE: ёмкость очереди задач
}

func loadPositions() (PositionsConfig, error) {
	checkInterval, err := getDurationEnv("POSITION_CHECK_INTERVAL", 30*time.Second)
	if err != nil {
		return PositionsConfig{}, err
	}
	staleAfter, err := getDurationEnv("POSITION_STALE_AFTER", 60*time.Second)
	if err != nil {
		return PositionsConfig{}, err
	}
	workers, err := getInt32Env("POSITION_WORKERS", 4)
	if err != nil {
		return PositionsConfig{}, err
	}
	queueSize, err := getInt32Env("POSITION_QUEUE_SIZE", 100)
	if err != nil {
		return PositionsConfig{}, err
	}
	// getInt32Env сам отклоняет 0 и отрицательные: пул без воркеров и
	// очередь нулевой ёмкости — ошибка конфигурации, а не режим работы.
	return PositionsConfig{
		CheckInterval: checkInterval,
		StaleAfter:    staleAfter,
		Workers:       int(workers),
		QueueSize:     int(queueSize),
	}, nil
}
