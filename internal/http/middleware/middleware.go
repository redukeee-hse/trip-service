// Package middleware — обёртки над роутером. Порядок фиксирован ТЗ:
//
//	Recover → Tracing → Logging → Metrics → роутер
//
// Tracing и Metrics подключаются вместе с OpenTelemetry.
package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// unknownRoute пишется в route, если запрос не попал ни в один маршрут
// (404 от роутера). Сырой путь не пишем: в нём могут быть id.
const unknownRoute = "unknown"

// withRouteContext заранее кладёт в запрос пустой контекст маршрута chi.
//
// chi заполняет шаблон маршрута (/api/v1/trips/{tripId}) в своём контексте,
// но если контекста нет, создаёт новый в копии запроса, и внешние middleware
// его не видят. Если же контекст уже есть, chi заполняет именно его, и после
// next.ServeHTTP шаблон доступен снаружи.
func withRouteContext(r *http.Request) *http.Request {
	if chi.RouteContext(r.Context()) != nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
}

// routePattern — шаблон маршрута после обработки запроса роутером.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return unknownRoute
}

// statusRecorder запоминает, что на самом деле ушло клиенту: код ответа и
// ошибку записи тела.
//
// net/http отправляет клиенту первый финальный WriteHeader, а повторные
// игнорирует. Write без WriteHeader сам отправляет 200. Поэтому код
// фиксируется один раз — при первом финальном WriteHeader или первом Write —
// и дальше не меняется: иначе после WriteHeader(201); WriteHeader(500) клиент
// получил бы 201, а логи и метрики показали бы 500.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	writeErr    error
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	// Если хендлер ничего не записал, net/http отправит 200.
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (s *statusRecorder) WriteHeader(code int) {
	// 1xx (кроме 101 Switching Protocols) — информационные ответы, после них
	// ещё будет финальный код, поэтому их не запоминаем.
	informational := code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
	if !s.wroteHeader && !informational {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	// Первый Write без WriteHeader отправляет 200 — дальше код не меняется.
	s.wroteHeader = true
	n, err := s.ResponseWriter.Write(b)
	// Запоминаем первую ошибку записи (обычно клиент закрыл соединение):
	// хендлер её отдать некому, а лог должен показать, что ответ не дошёл.
	if err != nil && s.writeErr == nil {
		s.writeErr = err
	}
	return n, err
}

// Unwrap нужен http.ResponseController, чтобы добраться до исходного writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
