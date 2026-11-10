package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/infra/telemetry"
)

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

// spanContext кладёт в ctx валидный span — так, как это сделает otelhttp.
func spanContext(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(testTraceID)
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex(testSpanID)
	if err != nil {
		t.Fatal(err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("строка лога не JSON: %q: %v", line, err)
		}
		records = append(records, m)
	}
	return records
}

func TestNewLogger_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ctx       func(t *testing.T) context.Context
		wantTrace bool
	}{
		{name: "inside span", ctx: spanContext, wantTrace: true},
		{name: "outside span", ctx: func(*testing.T) context.Context { return context.Background() }, wantTrace: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			log := telemetry.NewLogger(&buf, slog.LevelInfo, "trip-service")

			log.InfoContext(tt.ctx(t), "trip created")

			records := decodeLines(t, &buf)
			if len(records) != 1 {
				t.Fatalf("records = %d, want 1", len(records))
			}
			rec := records[0]
			for _, key := range []string{"time", "level", "msg"} {
				if _, ok := rec[key]; !ok {
					t.Errorf("нет обязательного поля %q", key)
				}
			}
			if rec["service"] != "trip-service" {
				t.Errorf("service = %v, want trip-service", rec["service"])
			}

			_, hasTrace := rec["trace_id"]
			_, hasSpan := rec["span_id"]
			if hasTrace != tt.wantTrace || hasSpan != tt.wantTrace {
				t.Fatalf("trace_id/span_id present = %v/%v, want %v", hasTrace, hasSpan, tt.wantTrace)
			}
			if tt.wantTrace && (rec["trace_id"] != testTraceID || rec["span_id"] != testSpanID) {
				t.Errorf("trace_id/span_id = %v/%v, want %s/%s", rec["trace_id"], rec["span_id"], testTraceID, testSpanID)
			}
		})
	}
}

// После logger.With обёртка не должна теряться: иначе логгер с доп. полями
// (а так делает любой компонент) молча перестанет писать trace_id.
func TestNewLogger_TraceSurvivesWithAndGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo, "trip-service").
		With(slog.String("component", "usecase")).
		WithGroup("details")

	log.InfoContext(spanContext(t), "trip completed", slog.String("trip_id", "42"))

	rec := decodeLines(t, &buf)[0]
	if rec["trace_id"] != testTraceID {
		t.Errorf("trace_id потерян после With/WithGroup: %v", rec)
	}
	if rec["component"] != "usecase" {
		t.Errorf("component = %v, want usecase", rec["component"])
	}
}

func TestNewLogger_Level(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, slog.LevelInfo, "trip-service")

	log.Debug("hidden")
	log.Info("visible")

	records := decodeLines(t, &buf)
	if len(records) != 1 || records[0]["msg"] != "visible" {
		t.Errorf("при LOG_LEVEL=info debug-записи не пишутся, got %v", records)
	}
}
