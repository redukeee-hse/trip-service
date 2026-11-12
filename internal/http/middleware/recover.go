package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/redukeee-hse/avitoService/internal/http/response"
)

// Recover ловит панику в хендлере: логирует её со стектрейсом и отвечает
// 500 internal_error. Процесс не падает, соединение остаётся рабочим.
//
// Recover стоит первым, поэтому он же кладёт в запрос контекст маршрута chi:
// так шаблон маршрута увидят и Recover, и все middleware после него.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = withRouteContext(r)
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				// ErrAbortHandler — штатный способ прервать ответ, net/http
				// обрабатывает его сам и не считает ошибкой.
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("method", r.Method),
					slog.String("route", routePattern(r)),
					slog.String("panic", fmt.Sprint(p)),
					slog.String("stack", string(debug.Stack())),
				)
				response.Error(w, r, fmt.Errorf("panic: %v", p))
			}()
			next.ServeHTTP(w, r)
		})
	}
}
