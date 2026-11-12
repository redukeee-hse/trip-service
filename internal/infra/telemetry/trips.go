package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// meterName — имя инструментирующей библиотеки для метрик сервиса.
const meterName = "github.com/redukeee-hse/avitoService"

// TripMetrics — бизнес-счётчики поездок. Usecase видит их через свои узкие
// интерфейсы (TripCreated / TripCompleted) и не знает про OpenTelemetry.
//
// Отдельных счётчиков ошибок нет: driver_busy и trip_not_found видны в
// http.server.request.duration по лейблу http.response.status_code.
type TripMetrics struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
}

func NewTripMetrics(mp metric.MeterProvider) (*TripMetrics, error) {
	meter := mp.Meter(meterName)

	created, err := meter.Int64Counter("tripgo.trips.created",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Созданные поездки."))
	if err != nil {
		return nil, fmt.Errorf("tripgo.trips.created: %w", err)
	}

	completed, err := meter.Int64Counter("tripgo.trips.completed",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Завершённые поездки."))
	if err != nil {
		return nil, fmt.Errorf("tripgo.trips.completed: %w", err)
	}

	return &TripMetrics{created: created, completed: completed}, nil
}

func (m *TripMetrics) TripCreated(ctx context.Context) {
	m.created.Add(ctx, 1)
}

func (m *TripMetrics) TripCompleted(ctx context.Context) {
	m.completed.Add(ctx, 1)
}
