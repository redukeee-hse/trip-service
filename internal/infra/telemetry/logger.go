// Package telemetry настраивает сигналы наблюдаемости сервиса: логи, а дальше
// трейсы и метрики OpenTelemetry. Остальной код получает готовый логгер
// зависимостью и про устройство телеметрии не знает.
package telemetry

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger возвращает JSON-логгер с полем service в каждой записи.
//
// Если запись сделана внутри спана (через методы *Context: InfoContext,
// ErrorContext, LogAttrs), в неё добавляются trace_id и span_id: по ним
// запись в логе находится в Jaeger и наоборот.
func NewLogger(w io.Writer, level slog.Level, service string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(traceHandler{base: handler}).With(slog.String("service", service))
}

// traceHandler — обёртка над slog.Handler, которая достаёт span из ctx.
//
// trace_id и span_id должны лежать на верхнем уровне записи, даже если
// логгер создан через WithGroup: иначе они окажутся внутри группы и поиск
// по trace_id не найдёт запись. Поэтому обёртка хранит исходный handler
// и цепочку With/WithGroup, а trace-поля добавляет до этой цепочки.
type traceHandler struct {
	base  slog.Handler
	chain []func(slog.Handler) slog.Handler
}

func (h traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	handler := h.base
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		handler = handler.WithAttrs([]slog.Attr{
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		})
	}
	for _, apply := range h.chain {
		handler = apply(handler)
	}
	return handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(func(next slog.Handler) slog.Handler { return next.WithAttrs(attrs) })
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return h.with(func(next slog.Handler) slog.Handler { return next.WithGroup(name) })
}

// with возвращает копию с новым шагом цепочки. Срез копируется, чтобы
// логгеры, порождённые от одного родителя, не делили общий массив.
func (h traceHandler) with(step func(slog.Handler) slog.Handler) traceHandler {
	chain := make([]func(slog.Handler) slog.Handler, 0, len(h.chain)+1)
	chain = append(chain, h.chain...)
	return traceHandler{base: h.base, chain: append(chain, step)}
}
