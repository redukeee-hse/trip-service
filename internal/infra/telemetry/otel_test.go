package telemetry

import (
	"context"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/redukeee-hse/avitoService/internal/config"
)

func TestNewSampler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantPrefix string // начало Description() сэмплера SDK
	}{
		{name: "always_on", wantPrefix: "AlwaysOnSampler"},
		{name: "always_off", wantPrefix: "AlwaysOffSampler"},
		{name: "traceidratio", wantPrefix: "TraceIDRatioBased{0.5}"},
		{name: "parentbased_always_on", wantPrefix: "ParentBased{root:AlwaysOnSampler"},
		{name: "parentbased_always_off", wantPrefix: "ParentBased{root:AlwaysOffSampler"},
		{name: "parentbased_traceidratio", wantPrefix: "ParentBased{root:TraceIDRatioBased{0.5}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := newSampler(tt.name, 0.5)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got := s.Description(); len(got) < len(tt.wantPrefix) || got[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("Description() = %q, want prefix %q", got, tt.wantPrefix)
			}
		})
	}

	if _, err := newSampler("sometimes", 1); err == nil {
		t.Error("unknown sampler must fail")
	}
}

func TestSignalURL(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{"http://localhost:4318", "http://localhost:4318/"} {
		if got := signalURL(endpoint, "traces"); got != "http://localhost:4318/v1/traces" {
			t.Errorf("signalURL(%q) = %q", endpoint, got)
		}
	}
}

// Setup не ходит в сеть: экспортёры подключаются лениво. Поэтому сервис
// стартует и при выключенном коллекторе. На остановке экспортёр пытается
// дописать спаны и ретраит, но не дольше переданного таймаута
// (в сервисе — SHUTDOWN_TIMEOUT), так что процесс всё равно завершится.
func TestSetup_CollectorUnavailable(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{config.ProtocolHTTPProtobuf, config.ProtocolGRPC} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			p, err := Setup(ctx, config.TelemetryConfig{
				ServiceName: "trip-service",
				Endpoint:    "http://127.0.0.1:1", // никто не слушает
				Protocol:    protocol,
				Sampler:     "always_on",
				SamplerArg:  1,
			}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}

			_, span := p.Tracer.Tracer("test").Start(ctx, "op")
			span.End()

			const timeout = time.Second
			shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()

			// Ошибка экспорта ожидаема — коллектора нет. Важно, что Shutdown
			// возвращается вовремя, а не висит.
			_ = p.Shutdown(shutdownCtx)

			if elapsed := time.Since(start); elapsed > timeout+500*time.Millisecond {
				t.Errorf("Shutdown занял %v, а таймаут %v", elapsed, timeout)
			}
		})
	}
}

func TestNewResource(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=test")

	res, err := newResource(context.Background(), "trip-service")
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	want := map[string]string{
		"service.name":           "trip-service",
		"service.namespace":      "tripgo",
		"deployment.environment": "test", // окружение переопределяет значение по умолчанию
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if got["service.version"] == "" {
		t.Error("service.version пустой")
	}
}

func TestTripMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m, err := NewTripMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	m.TripCreated(ctx)
	m.TripCreated(ctx)
	m.TripCompleted(ctx)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok || !sum.IsMonotonic {
				t.Errorf("%s: want monotonic counter, got %T", metric.Name, metric.Data)
				continue
			}
			for _, dp := range sum.DataPoints {
				got[metric.Name] += dp.Value
				if dp.Attributes.Len() != 0 {
					t.Errorf("%s: счётчик без лейблов по ТЗ, got %v", metric.Name, dp.Attributes)
				}
			}
		}
	}
	if got["tripgo.trips.created"] != 2 || got["tripgo.trips.completed"] != 1 {
		t.Errorf("counters = %v, want created=2 completed=1", got)
	}
}
