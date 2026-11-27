package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PositionMetrics — метрики опроса устаревших поездок (docs/observability.md).
type PositionMetrics struct {
	saved         metric.Int64Counter
	staleChecks   metric.Int64Counter
	workersActive metric.Int64UpDownCounter
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
}

func NewPositionMetrics(mp metric.MeterProvider) (*PositionMetrics, error) {
	meter := mp.Meter(meterName)
	m := &PositionMetrics{}
	var err error

	if m.saved, err = meter.Int64Counter("tripgo.positions.saved",
		metric.WithUnit("{position}"),
		metric.WithDescription("Сохранённые координаты.")); err != nil {
		return nil, fmt.Errorf("tripgo.positions.saved: %w", err)
	}
	if m.staleChecks, err = meter.Int64Counter("tripgo.stale_checks",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Что сканер сделал с поездкой на тике: queued, duplicate, dropped.")); err != nil {
		return nil, fmt.Errorf("tripgo.stale_checks: %w", err)
	}
	if m.workersActive, err = meter.Int64UpDownCounter("tripgo.position_requests.workers.active",
		metric.WithUnit("{worker}"),
		metric.WithDescription("Сколько воркеров сейчас заняты.")); err != nil {
		return nil, fmt.Errorf("tripgo.position_requests.workers.active: %w", err)
	}
	if m.requests, err = meter.Int64Counter("tripgo.position_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Чем закончилась попытка запросить координату: success, error, cancelled.")); err != nil {
		return nil, fmt.Errorf("tripgo.position_requests: %w", err)
	}
	if m.duration, err = meter.Float64Histogram("tripgo.position_requests.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Сколько занял вызов Push Service."),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5)); err != nil {
		return nil, fmt.Errorf("tripgo.position_requests.duration: %w", err)
	}
	return m, nil
}

func (m *PositionMetrics) PositionSaved(ctx context.Context) {
	m.saved.Add(ctx, 1)
}

func (m *PositionMetrics) StaleCheck(ctx context.Context, result string) {
	m.staleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *PositionMetrics) WorkerBusy(ctx context.Context) {
	m.workersActive.Add(ctx, 1)
}

func (m *PositionMetrics) WorkerIdle(ctx context.Context) {
	m.workersActive.Add(ctx, -1)
}

// Request пишет итог попытки; для cancelled вызова не было, длительность не пишем.
func (m *PositionMetrics) Request(ctx context.Context, result string, d time.Duration) {
	attrs := metric.WithAttributes(attribute.String("result", result))
	m.requests.Add(ctx, 1, attrs)
	if result != "cancelled" {
		m.duration.Record(ctx, d.Seconds(), attrs)
	}
}

// RegisterQueueSize регистрирует gauge tripgo.position_requests.queue.size.
func RegisterQueueSize(mp metric.MeterProvider, size func() int) error {
	_, err := mp.Meter(meterName).Int64ObservableGauge("tripgo.position_requests.queue.size",
		metric.WithUnit("{task}"),
		metric.WithDescription("Сколько задач сейчас в очереди опроса."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(size()))
			return nil
		}))
	if err != nil {
		return fmt.Errorf("tripgo.position_requests.queue.size: %w", err)
	}
	return nil
}
