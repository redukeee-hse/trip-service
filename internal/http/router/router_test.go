package router_test

// Сквозной тест транспорта: настоящий сгенерированный роутер chi + настоящие
// ручки, вместо usecase — простые фейки. Проверяем проводку: маршрут попадает
// в свою ручку, параметры доходят разобранными, кривые параметры дают 400.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/health"
	"github.com/redukeee-hse/avitoService/internal/http/router"
	"github.com/redukeee-hse/avitoService/internal/http/trip/create"
	"github.com/redukeee-hse/avitoService/internal/http/trip/finish"
	"github.com/redukeee-hse/avitoService/internal/http/trip/get"
)

// fakes записывают, кто и с чем был вызван. Моки gomock для интерфейсов ручек
// лежат в их _test-пакетах и отсюда не видны, а фейка на одну строку хватает.
type fakes struct {
	created  []*uuid.UUID // ключи идемпотентности, с которыми звали create
	got      []uuid.UUID
	finished []uuid.UUID
	pingErr  error
}

type createFake struct{ f *fakes }

func (c createFake) Execute(_ context.Context, t trip.Trip, key *uuid.UUID) (trip.Trip, bool, error) {
	c.f.created = append(c.f.created, key)
	t.ID = uuid.New()
	t.Status = trip.StatusActive
	return t, true, nil
}

type getFake struct{ f *fakes }

func (g getFake) Execute(_ context.Context, id uuid.UUID) (trip.Trip, error) {
	g.f.got = append(g.f.got, id)
	return trip.Trip{ID: id, Status: trip.StatusActive}, nil
}

type finishFake struct{ f *fakes }

func (g finishFake) Execute(_ context.Context, id uuid.UUID) (trip.Trip, error) {
	g.f.finished = append(g.f.finished, id)
	return trip.Trip{ID: id, Status: trip.StatusCompleted}, nil
}

type pingFake struct{ f *fakes }

func (p pingFake) Ping(context.Context) error { return p.f.pingErr }

func newRouter(f *fakes) http.Handler {
	return router.New(router.Handlers{
		Create: create.New(createFake{f}),
		Get:    get.New(getFake{f}),
		Finish: finish.New(finishFake{f}),
		Health: health.New(pingFake{f}, time.Second),
	})
}

func do(h http.Handler, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const tripsPath = "/api/v1/trips"

const validBody = `{
	"user_id": "11111111-1111-1111-1111-111111111111",
	"driver_id": "22222222-2222-2222-2222-222222222222",
	"start_point": {"latitude": 55.75, "longitude": 37.61},
	"end_point": {"latitude": 59.94, "longitude": 30.31},
	"price": 1450
}`

func TestRoutes(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	key := uuid.New()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		header     map[string]string
		pingErr    error
		wantStatus int
		check      func(t *testing.T, f *fakes)
	}{
		{
			name: "POST /api/v1/trips → create, ключ из заголовка разобран", method: http.MethodPost,
			path: tripsPath, body: validBody, header: map[string]string{"Idempotency-Key": key.String()},
			wantStatus: http.StatusCreated,
			check: func(t *testing.T, f *fakes) {
				if len(f.created) != 1 || f.created[0] == nil || *f.created[0] != key {
					t.Errorf("create вызван с ключами %v, want [%s]", f.created, key)
				}
			},
		},
		{
			name: "POST /api/v1/trips без ключа → в usecase nil", method: http.MethodPost,
			path: tripsPath, body: validBody,
			wantStatus: http.StatusCreated,
			check: func(t *testing.T, f *fakes) {
				if len(f.created) != 1 || f.created[0] != nil {
					t.Errorf("create вызван с ключами %v, want [nil]", f.created)
				}
			},
		},
		{
			name: "GET /api/v1/trips/{id} → get", method: http.MethodGet,
			path: "/api/v1/trips/" + id.String(), wantStatus: http.StatusOK,
			check: func(t *testing.T, f *fakes) {
				if len(f.got) != 1 || f.got[0] != id || len(f.finished) != 0 {
					t.Errorf("get = %v, finish = %v; want get [%s]", f.got, f.finished, id)
				}
			},
		},
		{
			name: "POST /api/v1/trips/{id}/finish → finish", method: http.MethodPost,
			path: "/api/v1/trips/" + id.String() + "/finish", wantStatus: http.StatusOK,
			check: func(t *testing.T, f *fakes) {
				if len(f.finished) != 1 || f.finished[0] != id || len(f.got) != 0 {
					t.Errorf("finish = %v, get = %v; want finish [%s]", f.finished, f.got, id)
				}
			},
		},
		{name: "GET /health", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
		{name: "GET /ready, БД жива", method: http.MethodGet, path: "/ready", wantStatus: http.StatusOK},
		{
			name: "GET /ready, БД лежит", method: http.MethodGet, path: "/ready",
			pingErr: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakes{pingErr: tt.pingErr}
			rec := do(newRouter(f), tt.method, tt.path, tt.body, tt.header)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

// Кривые параметры отсекает сгенерированный wrapper ещё до ручки.
// Ответ — наш problem+json invalid_request, а не text/plain от oapi-codegen.
func TestInvalidParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		header map[string]string
	}{
		{name: "tripId не UUID в get", method: http.MethodGet, path: "/api/v1/trips/42"},
		{name: "tripId не UUID в finish", method: http.MethodPost, path: "/api/v1/trips/abc/finish"},
		{
			name: "Idempotency-Key не UUID", method: http.MethodPost, path: tripsPath,
			header: map[string]string{"Idempotency-Key": "not-a-uuid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakes{}
			rec := do(newRouter(f), tt.method, tt.path, validBody, tt.header)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
			var p api.Problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("тело не problem+json: %v", err)
			}
			if p.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", p.Code)
			}
			if len(f.created)+len(f.got)+len(f.finished) != 0 {
				t.Error("запрос с кривым параметром дошёл до usecase")
			}
		})
	}
}
