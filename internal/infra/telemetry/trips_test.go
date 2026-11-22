package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/infra/telemetry"
)

// collectActive собирает метрики и возвращает точки gauge tripgo.trips.active.
func collectActive(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.DataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "tripgo.trips.active" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("tripgo.trips.active: want gauge, got %T", m.Data)
			}
			return gauge.DataPoints
		}
	}
	return nil
}

// Значение gauge спрашивается у хранилища в момент сбора, а не копится
// в памяти: каждый сбор — свежий запрос.
func TestRegisterActiveTrips(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	trips := NewMockactiveTripsCounter(ctrl)
	gomock.InOrder(
		trips.EXPECT().CountActive(gomock.Any()).Return(int64(3), nil),
		trips.EXPECT().CountActive(gomock.Any()).Return(int64(1), nil),
	)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	if err := telemetry.RegisterActiveTrips(mp, trips, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}

	for _, want := range []int64{3, 1} {
		points := collectActive(t, reader)
		if len(points) != 1 || points[0].Value != want {
			t.Fatalf("tripgo.trips.active = %v, want %d", points, want)
		}
		if points[0].Attributes.Len() != 0 {
			t.Errorf("gauge без лейблов, got %v", points[0].Attributes)
		}
	}
}

// БД не ответила — точку не отдаём, а не 0: на графике ноль выглядел бы как
// «активных поездок нет». Ошибка уходит в лог как Warn.
func TestRegisterActiveTrips_StoreError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	trips := NewMockactiveTripsCounter(ctrl)
	trips.EXPECT().CountActive(gomock.Any()).Return(int64(0), errors.New("connection refused"))

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	var logs bytes.Buffer
	if err := telemetry.RegisterActiveTrips(mp, trips, slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}

	if points := collectActive(t, reader); len(points) != 0 {
		t.Errorf("при ошибке БД точек быть не должно, got %v", points)
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"level":"WARN"`)) || !bytes.Contains(logs.Bytes(), []byte("connection refused")) {
		t.Errorf("ошибка сбора не попала в лог как Warn: %s", logs.String())
	}
}
