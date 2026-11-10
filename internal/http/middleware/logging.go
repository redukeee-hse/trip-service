package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/redukeee-hse/avitoService/internal/http/requesterror"
)

// Logging пишет одну запись на запрос, когда ответ уже готов.
//
// В записи только метаданные: метод, шаблон маршрута, статус, длительность.
// Тела запроса и ответа не логируем: там персональные данные. Уровень Info
// для 2xx и 4xx (ошибка клиента — не поломка сервиса), Error — для 5xx, и
// тогда в запись добавляется внутренняя ошибка, которую клиент не увидел.
func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			r = withRouteContext(r)
			ctx, reportedErr := requesterror.Attach(r.Context())
			r = r.WithContext(ctx)
			rec := newStatusRecorder(w)

			next.ServeHTTP(rec, r)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", routePattern(r)),
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			}
			level := slog.LevelInfo
			if rec.status >= http.StatusInternalServerError {
				level = slog.LevelError
				if err := reportedErr(); err != nil {
					attrs = append(attrs, slog.String("error", err.Error()))
				}
			}
			log.LogAttrs(r.Context(), level, "request handled", attrs...)
		})
	}
}
