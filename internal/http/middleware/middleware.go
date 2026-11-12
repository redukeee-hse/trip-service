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

// statusRecorder запоминает код ответа, который записал хендлер.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	// Если хендлер не вызвал WriteHeader, net/http отправит 200.
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap нужен http.ResponseController, чтобы добраться до исходного writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
