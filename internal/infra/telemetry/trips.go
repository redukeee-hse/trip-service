package telemetry

import (
	"context"
	"fmt"
	"log/slog"

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

// RegisterActiveTrips регистрирует gauge tripgo.trips.active — сколько
// поездок сейчас в статусе active.
//
// Это observable gauge: значение не считается на каждом событии, а
// спрашивается у БД в момент сбора метрик (раз в OTEL_METRIC_EXPORT_INTERVAL).
// Up-down counter (+1 при создании, -1 при завершении) жил бы в памяти одной
// копии сервиса: обнулялся бы при рестарте и не видел бы поездок, созданных
// другими копиями или изменённых мимо сервиса.
//
// Если БД не ответила, точка за этот интервал пропускается и пишется Warn:
// отдавать 0 нельзя — на графике это выглядело бы как «активных поездок нет».
func RegisterActiveTrips(mp metric.MeterProvider, trips activeTripsCounter, log *slog.Logger) error {
	_, err := mp.Meter(meterName).Int64ObservableGauge("tripgo.trips.active",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Поездки в статусе active на момент сбора метрик."),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			n, err := trips.CountActive(ctx)
			if err != nil {
				log.WarnContext(ctx, "tripgo.trips.active not collected", slog.String("error", err.Error()))
				return nil
			}
			o.Observe(n)
			return nil
		}))
	if err != nil {
		return fmt.Errorf("tripgo.trips.active: %w", err)
	}
	return nil
}
