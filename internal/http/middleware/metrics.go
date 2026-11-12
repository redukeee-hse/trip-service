package middleware

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/redukeee-hse/avitoService/internal/http/middleware"

// durationBuckets — границы корзин гистограммы в секундах из семантических
// конвенций OTel. Стандартные корзины SDK рассчитаны на миллисекунды
// (5, 10, 25 ...) и для секунд бесполезны: всё попало бы в первую.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// Metrics пишет HTTP-метрики сервера:
//
//   - http.server.request.duration (histogram, секунды): метод, шаблон
//     маршрута, код ответа. Из неё считают RPS, перцентили и долю ошибок;
//   - http.server.active_requests (up-down counter): метод и маршрут.
//     Растёт, когда сервис не успевает обрабатывать запросы.
//
// В http.route — только шаблон: с tripId на каждую поездку завелась бы
// своя временная серия, и метрика перестала бы агрегироваться.
func Metrics(mp metric.MeterProvider, routes RouteFinder) (func(http.Handler) http.Handler, error) {
	meter := mp.Meter(meterName)

	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Длительность обработки HTTP-запроса."),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, fmt.Errorf("http.server.request.duration: %w", err)
	}

	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Запросы, которые обрабатываются прямо сейчас."))
	if err != nil {
		return nil, fmt.Errorf("http.server.active_requests: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()
			method := attribute.String("http.request.method", normalizeMethod(r.Method))
			route := attribute.String("http.route", findRoute(routes, r))
			activeAttrs := metric.WithAttributes(method, route)

			active.Add(ctx, 1, activeAttrs)
			// defer: счётчик должен вернуться, даже если хендлер запаникует.
			defer active.Add(ctx, -1, activeAttrs)

			rec := newStatusRecorder(w)
			next.ServeHTTP(rec, r)

			duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
				method, route, attribute.Int("http.response.status_code", rec.status),
			))
		})
	}, nil
}
