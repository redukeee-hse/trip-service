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

			// Запись в defer: при панике хендлера итоговая запись всё равно
			// пишется — со статусом 500 и error=panic. Саму панику со
			// стектрейсом логирует Recover, с тем же trace_id.
			completed := false
			defer func() {
				log.LogAttrs(r.Context(), requestLevel(rec, completed), "request handled",
					requestAttrs(r, rec, completed, start, reportedErr)...)
			}()
			next.ServeHTTP(rec, r)
			completed = true
		})
	}
}

// requestAttrs — поля записи "request handled". completed=false значит, что
// хендлер запаниковал и клиент получит 500 от Recover.
func requestAttrs(r *http.Request, rec *statusRecorder, completed bool, start time.Time, reportedErr func() error) []slog.Attr {
	status := rec.status
	if !completed {
		status = http.StatusInternalServerError
	}
	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("route", routePattern(r)),
		slog.Int("status", status),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	}
	switch {
	case !completed:
		attrs = append(attrs, slog.String("error", "panic"))
	case status >= http.StatusInternalServerError:
		if err := reportedErr(); err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}
	}
	// Ответ не дошёл до клиента (обычно клиент оборвал соединение) — без
	// этого поля такой запрос выглядел бы в логе успешным.
	if rec.writeErr != nil {
		attrs = append(attrs, slog.String("write_error", rec.writeErr.Error()))
	}
	return attrs
}

// requestLevel: Error для 5xx и паники; Warn, если ответ не удалось записать
// (деградация, но сервис исправен); Info для остальных 2xx и 4xx — ошибка
// клиента не поломка сервиса.
func requestLevel(rec *statusRecorder, completed bool) slog.Level {
	switch {
	case !completed || rec.status >= http.StatusInternalServerError:
		return slog.LevelError
	case rec.writeErr != nil:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
