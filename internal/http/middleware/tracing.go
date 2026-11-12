package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing создаёт корневой спан на каждый входящий запрос.
//
// Имя спана — "{method} {http.route}", то есть POST /api/v1/trips, а не
// полный URL с id: так все запросы одной ручки группируются в Jaeger.
// Из заголовка traceparent продолжается трейс вызывающего сервиса.
// Спаны запросов в БД (otelpgx) становятся дочерними к этому спану,
// потому что ctx со спаном уходит дальше в хендлер, usecase и репозиторий.
//
// HTTP-метрики otelhttp выключены: их пишет отдельный middleware Metrics,
// как требует порядок Recover → Tracing → Logging → Metrics.
func Tracing(tp trace.TracerProvider, propagator propagation.TextMapPropagator, routes RouteFinder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.route", findRoute(routes, r)))
			// Recover стоит снаружи и поймает панику уже после того, как спан
			// закрыт. Поэтому помечаем спан здесь и пробрасываем панику дальше.
			defer func() {
				if p := recover(); p != nil {
					span.SetStatus(codes.Error, "panic")
					span.SetAttributes(attribute.String("error.type", "panic"))
					panic(p)
				}
			}()
			next.ServeHTTP(w, r)
		})
		return otelhttp.NewHandler(withRoute, "http.server",
			otelhttp.WithTracerProvider(tp),
			otelhttp.WithPropagators(propagator),
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return normalizeMethod(r.Method) + " " + findRoute(routes, r)
			}),
		)
	}
}
