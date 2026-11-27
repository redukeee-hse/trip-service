package telemetry_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/redukeee-hse/avitoService/internal/infra/telemetry"
)

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]metricdata.Aggregation{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m.Data
		}
	}
	return out
}

// sumByResult — значения int64-счётчика по лейблу result.
func sumByResult(t *testing.T, data metricdata.Aggregation) map[string]int64 {
	t.Helper()
	sum, ok := data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("want Sum[int64], got %T", data)
	}
	out := map[string]int64{}
	for _, dp := range sum.DataPoints {
		r, _ := dp.Attributes.Value(attribute.Key("result"))
		out[r.AsString()] += dp.Value
	}
	return out
}

// Имена, типы и лейблы — ровно как в docs/observability.md: по ним строятся
// дашборды, опечатка в имени = пустой график.
func TestPositionMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := telemetry.NewPositionMetrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	queueLen := 7
	if err := telemetry.RegisterQueueSize(mp, func() int { return queueLen }); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	m.PositionSaved(ctx)
	m.PositionSaved(ctx)
	m.StaleCheck(ctx, "queued")
	m.StaleCheck(ctx, "queued")
	m.StaleCheck(ctx, "dropped")
	m.WorkerBusy(ctx)
	m.WorkerBusy(ctx)
	m.WorkerIdle(ctx)
	m.Request(ctx, "success", 200*time.Millisecond)
	m.Request(ctx, "error", time.Second)
	m.Request(ctx, "cancelled", 0)

	got := collect(t, reader)

	if s, ok := got["tripgo.positions.saved"].(metricdata.Sum[int64]); !ok || s.DataPoints[0].Value != 2 {
		t.Errorf("tripgo.positions.saved = %+v, want 2", got["tripgo.positions.saved"])
	}
	if r := sumByResult(t, got["tripgo.stale_checks"]); r["queued"] != 2 || r["dropped"] != 1 {
		t.Errorf("tripgo.stale_checks = %v, want queued=2 dropped=1", r)
	}
	active, ok := got["tripgo.position_requests.workers.active"].(metricdata.Sum[int64])
	if !ok || active.IsMonotonic || active.DataPoints[0].Value != 1 {
		t.Errorf("workers.active = %+v, want немонотонный 1", got["tripgo.position_requests.workers.active"])
	}
	if r := sumByResult(t, got["tripgo.position_requests"]); r["success"] != 1 || r["error"] != 1 || r["cancelled"] != 1 {
		t.Errorf("tripgo.position_requests = %v, want по 1", r)
	}
	hist, ok := got["tripgo.position_requests.duration"].(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("duration: want Histogram[float64], got %T", got["tripgo.position_requests.duration"])
	}
	var count uint64
	for _, dp := range hist.DataPoints {
		count += dp.Count
	}
	if count != 2 {
		t.Errorf("в гистограмме %d измерений, want 2: cancelled вызова не делал", count)
	}
	gauge, ok := got["tripgo.position_requests.queue.size"].(metricdata.Gauge[int64])
	if !ok || gauge.DataPoints[0].Value != 7 {
		t.Errorf("queue.size = %+v, want 7", got["tripgo.position_requests.queue.size"])
	}
}
