package middleware_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/http/middleware"
	"github.com/redukeee-hse/avitoService/internal/http/response"
)

// telemetryApp — цепочка как в container: Recover → Tracing → Logging →
// Metrics → роутер, но с провайдерами, которые пишут в память.
type telemetryApp struct {
	handler http.Handler
	spans   *tracetest.SpanRecorder
	metrics *sdkmetric.ManualReader
}

func newTelemetryApp(t *testing.T) telemetryApp {
	t.Helper()

	r := chi.NewRouter()
	r.Get(tripRoute, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "internal":
			response.Error(w, r, errors.New("pq: connection refused"))
		case casePanic:
			panic("boom")
		case "slow":
			w.WriteHeader(http.StatusNoContent)
		default:
			response.JSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "tripId")})
		}
	})

	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	var logs bytes.Buffer
	log := newLogger(&logs)

	metrics, err := middleware.Metrics(mp, r)
	if err != nil {
		t.Fatal(err)
	}
	propagator := propagation.TraceContext{}
	h := middleware.Recover(log)(middleware.Tracing(tp, propagator, r)(middleware.Logging(log)(metrics(r))))

	return telemetryApp{handler: h, spans: spans, metrics: reader}
}

func (a telemetryApp) do(method, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func spanAttrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func onlySpan(t *testing.T, spans *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("spans = %d, want 1", len(ended))
	}
	return ended[0]
}

func TestTracing_SpanNameAndRoute(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	rec := app.do(http.MethodGet, "/api/v1/trips/"+tripID, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	span := onlySpan(t, app.spans)
	// Имя — шаблон маршрута, а не URL с id: иначе в Jaeger на каждую
	// поездку была бы своя «операция».
	if want := "GET " + tripRoute; span.Name() != want {
		t.Errorf("span name = %q, want %q", span.Name(), want)
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("kind = %v, want server", span.SpanKind())
	}
	if got := spanAttrs(span)["http.route"].AsString(); got != tripRoute {
		t.Errorf("http.route = %q, want %q", got, tripRoute)
	}
	if span.Status().Code == codes.Error {
		t.Error("успешный запрос не должен помечать спан ошибкой")
	}
}

// Входящий traceparent продолжает трейс вызывающего сервиса.
func TestTracing_ContinuesIncomingTrace(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	app.do(http.MethodGet, "/api/v1/trips/"+tripID, map[string]string{"traceparent": parent})

	span := onlySpan(t, app.spans)
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %s, want из traceparent", got)
	}
}

func TestTracing_InternalErrorMarksSpan(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	app.do(http.MethodGet, "/api/v1/trips/"+tripID+"?case=internal", nil)

	span := onlySpan(t, app.spans)
	if span.Status().Code != codes.Error {
		t.Errorf("status = %v, want Error", span.Status().Code)
	}
	if got := spanAttrs(span)["error.type"].AsString(); got != "internal_error" {
		t.Errorf("error.type = %q, want internal_error", got)
	}
	var recorded bool
	for _, ev := range span.Events() {
		if ev.Name == "exception" {
			recorded = true
		}
	}
	if !recorded {
		t.Error("ошибка не записана в спан событием exception")
	}
}

func TestTracing_PanicMarksSpan(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	rec := app.do(http.MethodGet, "/api/v1/trips/"+tripID+"?case=panic", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 от Recover", rec.Code)
	}
	span := onlySpan(t, app.spans)
	if span.Status().Code != codes.Error || spanAttrs(span)["error.type"].AsString() != casePanic {
		t.Errorf("status/error.type = %v/%q, want Error/panic", span.Status().Code, spanAttrs(span)["error.type"].AsString())
	}
}

// collectHTTP возвращает точки гистограммы длительности и счётчика активных запросов.
func collectHTTP(t *testing.T, reader *sdkmetric.ManualReader) ([]metricdata.HistogramDataPoint[float64], []metricdata.DataPoint[int64]) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var (
		hist   []metricdata.HistogramDataPoint[float64]
		active []metricdata.DataPoint[int64]
	)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "http.server.request.duration":
				if m.Unit != "s" {
					t.Errorf("unit = %q, want s", m.Unit)
				}
				hist = append(hist, m.Data.(metricdata.Histogram[float64]).DataPoints...)
			case "http.server.active_requests":
				sum := m.Data.(metricdata.Sum[int64])
				if sum.IsMonotonic {
					t.Error("active_requests должен быть up-down counter")
				}
				active = append(active, sum.DataPoints...)
			}
		}
	}
	return hist, active
}

func attrValue(set attribute.Set, key attribute.Key) string {
	v, _ := set.Value(key)
	return fmt.Sprint(v.AsInterface())
}

func TestMetrics_RequestDuration(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	app.do(http.MethodGet, "/api/v1/trips/"+tripID, nil)
	app.do(http.MethodGet, "/api/v1/trips/0b5b3a64-0000-0000-0000-000000000000", nil)
	app.do(http.MethodGet, "/api/v1/trips/"+tripID+"?case=internal", nil)

	hist, active := collectHTTP(t, app.metrics)

	counts := map[string]uint64{}
	for _, dp := range hist {
		if route := attrValue(dp.Attributes, "http.route"); route != tripRoute {
			t.Errorf("http.route = %q, want шаблон %q", route, tripRoute)
		}
		if m := attrValue(dp.Attributes, "http.request.method"); m != http.MethodGet {
			t.Errorf("method = %q", m)
		}
		counts[attrValue(dp.Attributes, "http.response.status_code")] += dp.Count
	}
	// Два разных id — одна временная серия: по ним не дробим.
	if counts["200"] != 2 || counts["500"] != 1 {
		t.Errorf("counts by status = %v, want 200:2 500:1", counts)
	}

	for _, dp := range active {
		if dp.Value != 0 {
			t.Errorf("после ответа active_requests = %d, want 0 (%v)", dp.Value, dp.Attributes)
		}
	}
}

func TestMetrics_UnknownRouteAndMethod(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	app.do(http.MethodGet, "/no/such/"+tripID, nil)
	app.do("BREW", "/api/v1/trips/"+tripID, nil)

	hist, _ := collectHTTP(t, app.metrics)
	seen := map[string]bool{}
	for _, dp := range hist {
		seen[attrValue(dp.Attributes, "http.request.method")+" "+attrValue(dp.Attributes, "http.route")] = true
	}
	// Произвольный путь и метод не раздувают число серий. Для BREW маршрута
	// нет (зарегистрирован только GET), поэтому и route — unknown.
	for _, want := range []string{"GET unknown", "_OTHER unknown"} {
		if !seen[want] {
			t.Errorf("нет серии %q, есть %v", want, seen)
		}
	}
}

// Счётчик активных запросов должен вернуться к нулю и после паники.
func TestMetrics_ActiveRequestsAfterPanic(t *testing.T) {
	t.Parallel()

	app := newTelemetryApp(t)
	app.do(http.MethodGet, "/api/v1/trips/"+tripID+"?case=panic", nil)

	_, active := collectHTTP(t, app.metrics)
	if len(active) == 0 {
		t.Fatal("active_requests не записан")
	}
	for _, dp := range active {
		if dp.Value != 0 {
			t.Errorf("active_requests = %d после паники, want 0", dp.Value)
		}
	}
}
