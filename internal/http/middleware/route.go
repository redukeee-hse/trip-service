package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RouteFinder ищет шаблон маршрута по методу и пути. Его реализует роутер
// chi (chi.Routes). Tracing и Metrics стоят до роутера и узнают маршрут
// заранее: имя спана задаётся при его создании, а счётчик активных запросов
// увеличивается до вызова хендлера.
type RouteFinder interface {
	Find(rctx *chi.Context, method, path string) string
}

// findRoute — шаблон маршрута запроса, например /api/v1/trips/{tripId},
// или unknownRoute, если такого маршрута нет. Подставленный id в
// результат не попадает никогда.
func findRoute(routes RouteFinder, r *http.Request) string {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	if pattern := routes.Find(chi.NewRouteContext(), r.Method, path); pattern != "" {
		return pattern
	}
	return unknownRoute
}

// knownMethods — методы, которые попадают в лейбл как есть. Остальные
// пишутся как _OTHER (семантические конвенции OTel): иначе клиент с
// произвольным методом раздует число временных рядов.
var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodConnect: true,
	http.MethodOptions: true, http.MethodTrace: true,
}

func normalizeMethod(method string) string {
	if knownMethods[method] {
		return method
	}
	return "_OTHER"
}
