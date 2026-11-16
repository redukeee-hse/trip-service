package middleware

import (
	"context"
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
// Recover — единственное место, где вызывается recover(), и панику он дальше
// не пробрасывает. Внутренние middleware (Tracing, Logging, Metrics) панику не
// перехватывают: они замечают её по флагу в defer и дописывают свою часть
// телеметрии — спан с ошибкой, запись лога, точку в гистограмме — пока паника
// поднимается к Recover.
//
// Recover стоит первым, поэтому он же кладёт в запрос контекст маршрута chi и
// ячейку под контекст со спаном. Tracing записывает в неё ctx своего спана,
// и запись panic recovered получает тот же trace_id, что и запрос.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = withRouteContext(r)
			slot := &traceSlot{}
			r = r.WithContext(context.WithValue(r.Context(), traceSlotKey{}, slot))

			defer func() {
				p := recover()
				if p == nil {
					return
				}
				logCtx := r.Context()
				if slot.ctx != nil {
					logCtx = slot.ctx
				}

				// ErrAbortHandler — штатный способ прервать ответ: хендлер сам
				// решил оборвать соединение. Это не поломка, стектрейс и 500 не
				// нужны; ответ уже частично отправлен, дописывать его нельзя.
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					log.LogAttrs(logCtx, slog.LevelDebug, "request aborted",
						slog.String("method", r.Method),
						slog.String("route", routePattern(r)),
					)
					return
				}

				log.LogAttrs(logCtx, slog.LevelError, "panic recovered",
					slog.String("method", r.Method),
					slog.String("route", routePattern(r)),
					slog.String("panic", fmt.Sprint(p)),
					// Внутренние middleware панику не перехватывали, поэтому
					// стек ещё содержит кадры, где она произошла.
					slog.String("stack", string(debug.Stack())),
				)
				response.Error(w, r, fmt.Errorf("panic: %v", p))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// traceSlot — ячейка, через которую Tracing передаёт Recover контекст со
// спаном. Recover стоит снаружи Tracing и сам спана не видит: ctx в Go
// передаётся только внутрь, а не наружу. Указатель в ctx позволяет внутреннему
// middleware оставить значение для внешнего.
type traceSlot struct {
	ctx context.Context
}

type traceSlotKey struct{}

// rememberTraceContext сохраняет ctx со спаном для Recover. Без Recover в
// цепочке (например, в тестах) ничего не делает.
func rememberTraceContext(ctx context.Context) {
	if slot, ok := ctx.Value(traceSlotKey{}).(*traceSlot); ok {
		slot.ctx = ctx
	}
}
