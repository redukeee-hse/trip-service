// Package router связывает сгенерированный по OpenAPI роутер с ручками.
//
// oapi-codegen генерирует один ServerInterface на все пять операций, а ручки
// у нас разложены по пакетам. Server — тонкий адаптер: реализует интерфейс и
// делегирует каждый метод своей ручке. Логики здесь нет, только проводка.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/health"
	"github.com/redukeee-hse/avitoService/internal/http/response"
	"github.com/redukeee-hse/avitoService/internal/http/trip/create"
	"github.com/redukeee-hse/avitoService/internal/http/trip/finish"
	"github.com/redukeee-hse/avitoService/internal/http/trip/get"
)

// Handlers — ручки, которые собирает container. Структура вместо пяти
// позиционных параметров: при новой ручке не надо переставлять аргументы,
// а имена полей не затеняют имена пакетов create/get/finish.
type Handlers struct {
	Create *create.Handler
	Get    *get.Handler
	Finish *finish.Handler
	Health *health.Handler
}

type server struct {
	h Handlers
}

// Компилятор проверит, что server реализует все методы ServerInterface.
// Появится в OpenAPI новая операция — сборка упадёт здесь, а не в рантайме.
var _ api.ServerInterface = (*server)(nil)

// New возвращает роутер со всеми маршрутами из OpenAPI. Тип chi.Router, а
// не http.Handler: middleware Tracing и Metrics спрашивают у него шаблон
// маршрута (Find) ещё до того, как запрос дойдёт до роутера.
func New(h Handlers) chi.Router {
	r := chi.NewRouter()
	api.HandlerWithOptions(&server{h: h}, api.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: invalidParam,
	})
	return r
}

// invalidParam вызывает сгенерированный wrapper, когда не смог разобрать
// параметр пути или заголовка: кривой UUID в {tripId} или в Idempotency-Key.
// Без него oapi-codegen ответил бы text/plain с текстом ошибки парсера.
func invalidParam(w http.ResponseWriter, r *http.Request, _ error) {
	response.InvalidRequest(w, r, "Invalid path or query parameter")
}

func (s *server) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	s.h.Create.Handle(w, r, params)
}

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.h.Get.Handle(w, r, tripID)
}

func (s *server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.h.Finish.Handle(w, r, tripID)
}

func (s *server) Health(w http.ResponseWriter, r *http.Request) {
	s.h.Health.Health(w, r)
}

func (s *server) Ready(w http.ResponseWriter, r *http.Request) {
	s.h.Health.Ready(w, r)
}
