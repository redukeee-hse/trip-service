package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/health"
)

const pingTimeout = 50 * time.Millisecond

func decode(t *testing.T, rec *httptest.ResponseRecorder) api.HealthResponse {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body api.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("тело не HealthResponse: %v", err)
	}
	return body
}

// Liveness не ходит в БД: мок без EXPECT уронит тест при любом Ping.
func TestHealth_DoesNotTouchDB(t *testing.T) {
	t.Parallel()

	db := NewMockpinger(gomock.NewController(t))
	rec := httptest.NewRecorder()

	health.New(db, pingTimeout).Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if body := decode(t, rec); body.Status != api.Ok {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ping       func(ctx context.Context) error
		wantStatus int
		wantBody   api.HealthResponseStatus
	}{
		{
			name: "БД отвечает → 200 ok",
			ping: func(ctx context.Context) error {
				// Пинг идёт с дедлайном не дальше таймаута: так ловим забытый
				// timeout в New (0 → дедлайн уже в прошлом) и пинг без дедлайна.
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("у контекста пинга нет дедлайна")
				}
				if left := time.Until(deadline); left <= 0 || left > pingTimeout {
					return errors.New("дедлайн пинга не соответствует таймауту")
				}
				return nil
			},
			wantStatus: http.StatusOK,
			wantBody:   api.Ok,
		},
		{
			name:       "БД недоступна → 503 unavailable",
			ping:       func(context.Context) error { return errors.New("dial tcp: connection refused") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   api.Unavailable,
		},
		{
			// БД зависла: пинг ждёт, пока не истечёт контекст. Без таймаута
			// ручка висела бы вечно — тест упал бы по общему таймауту go test.
			name: "БД зависла → таймаут → 503",
			ping: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   api.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := NewMockpinger(gomock.NewController(t))
			db.EXPECT().Ping(gomock.Any()).DoAndReturn(tt.ping).Times(1)
			rec := httptest.NewRecorder()

			health.New(db, pingTimeout).Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			// Ровно один JSON в теле: ловит пропущенный return после ошибки,
			// когда в ответ дописывается второй объект.
			dec := json.NewDecoder(rec.Body)
			var body api.HealthResponse
			if err := dec.Decode(&body); err != nil {
				t.Fatalf("тело не HealthResponse: %v", err)
			}
			if dec.More() {
				t.Error("в теле больше одного JSON-объекта: ответ записан дважды")
			}
			if body.Status != tt.wantBody {
				t.Errorf("status = %q, want %q", body.Status, tt.wantBody)
			}
		})
	}
}
