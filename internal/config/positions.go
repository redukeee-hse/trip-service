package config

import "time"

type PositionsConfig struct {
	CheckInterval time.Duration // POSITION_CHECK_INTERVAL
	StaleAfter    time.Duration // POSITION_STALE_AFTER
	Workers       int           // POSITION_WORKERS
	QueueSize     int           // POSITION_QUEUE_SIZE
	ScanBatchSize int           // POSITION_SCAN_BATCH_SIZE
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
	scanBatchSize, err := getInt32Env("POSITION_SCAN_BATCH_SIZE", 100)
	if err != nil {
		return PositionsConfig{}, err
	}
	return PositionsConfig{
		CheckInterval: checkInterval,
		StaleAfter:    staleAfter,
		Workers:       int(workers),
		QueueSize:     int(queueSize),
		ScanBatchSize: int(scanBatchSize),
	}, nil
}
