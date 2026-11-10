package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/middleware"
	"github.com/redukeee-hse/avitoService/internal/http/response"
)

const (
	casePanic = "panic"
	tripRoute = "/api/v1/trips/{tripId}"
	tripID    = "0b5b3a64-5d5c-4c1e-9d43-8d6b7f0c2a11"
)

// newApp собирает цепочку как в container: Recover → Logging → chi-роутер.
// Хендлер выбирается по ?case=..., чтобы проверить разные ответы на одном маршруте.
func newApp(log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Get(tripRoute, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "ok":
			// Параметр пути должен разобраться и при заранее созданном
			// контексте маршрута — проверяем, что chi его заполнил.
			response.JSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "tripId")})
		case "internal":
			response.Error(w, r, errors.New("pq: connection refused"))
		case casePanic:
			panic("nil map write")
		}
	})
	return middleware.Recover(log)(middleware.Logging(log)(r))
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("строка лога не JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestLogging_OneRecordWithRouteTemplate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := httptest.NewRecorder()

	newApp(newLogger(&buf)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID+"?case=ok", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), tripID) {
		t.Errorf("tripId не разобран роутером: %s", rec.Body.String())
	}

	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("records = %d, want ровно 1 на запрос", len(logs))
	}
	got := logs[0]
	want := map[string]any{
		"msg":    "request handled",
		"level":  "INFO",
		"method": "GET",
		"route":  tripRoute, // шаблон, а не путь с подставленным id
		"status": float64(http.StatusOK),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if _, ok := got["duration_ms"]; !ok {
		t.Error("нет duration_ms")
	}
	if strings.Contains(buf.String(), tripID) {
		t.Error("id поездки попал в лог: в route должен быть шаблон")
	}
}

func TestLogging_LevelAndErrorByStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantLevel string
		wantErr   string // пусто — поля error быть не должно
	}{
		{
			name: "4xx is info without error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response.InvalidRequest(w, r, "invalid JSON body")
			},
			wantLevel: "INFO",
		},
		{
			name: "5xx is error with internal details",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response.Error(w, r, errors.New("pq: connection refused"))
			},
			wantLevel: "ERROR",
			wantErr:   "pq: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			rec := httptest.NewRecorder()
			h := middleware.Logging(newLogger(&buf))(tt.handler)

			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/trips", nil))

			got := records(t, &buf)[0]
			if got["level"] != tt.wantLevel {
				t.Errorf("level = %v, want %v", got["level"], tt.wantLevel)
			}
			errAttr, hasErr := got["error"]
			if tt.wantErr == "" && hasErr {
				t.Errorf("error не должен попадать в лог для 4xx: %v", errAttr)
			}
			if tt.wantErr != "" && errAttr != tt.wantErr {
				t.Errorf("error = %v, want %v", errAttr, tt.wantErr)
			}
			// Детали ошибки — только в лог, не клиенту.
			if strings.Contains(rec.Body.String(), "connection refused") {
				t.Errorf("детали внутренней ошибки утекли в ответ: %s", rec.Body.String())
			}
		})
	}
}

func TestLogging_UnknownRoute(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	newApp(newLogger(&buf)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no/such/"+tripID, nil))

	got := records(t, &buf)[0]
	if got["route"] != "unknown" || got["status"] != float64(http.StatusNotFound) {
		t.Errorf("route/status = %v/%v, want unknown/404", got["route"], got["status"])
	}
}

func TestRecover(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := httptest.NewRecorder()

	newApp(newLogger(&buf)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID+"?case=panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body api.Problem
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("ответ не problem+json: %v", err)
	}
	if body.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", body.Code)
	}
	if body.Detail != nil && strings.Contains(*body.Detail, "nil map") {
		t.Error("текст паники утёк в ответ")
	}

	var panicLog map[string]any
	for _, r := range records(t, &buf) {
		if r["msg"] == "panic recovered" {
			panicLog = r
		}
	}
	if panicLog == nil {
		t.Fatal("паника не залогирована")
	}
	if panicLog["level"] != "ERROR" || panicLog["route"] != tripRoute {
		t.Errorf("level/route = %v/%v, want ERROR/%s", panicLog["level"], panicLog["route"], tripRoute)
	}
	if stack, _ := panicLog["stack"].(string); !strings.Contains(stack, "goroutine") {
		t.Error("в записи нет стектрейса")
	}
}

func TestRecover_ErrAbortHandlerIsRethrown(t *testing.T) {
	t.Parallel()

	h := middleware.Recover(newLogger(&bytes.Buffer{}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Errorf("recover() = %v, want http.ErrAbortHandler", p)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
