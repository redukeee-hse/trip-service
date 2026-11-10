package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/requesterror"
	"github.com/redukeee-hse/avitoService/internal/http/response"
)

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var p api.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("тело не problem+json: %v", err)
	}
	return p
}

func TestJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusCreated, map[string]string{"status": "ok"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got["status"] != "ok" {
		t.Errorf("body = %v, err = %v", got, err)
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/42", nil)

	response.Error(rec, req, trip.ErrTripNotFound)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Code != "trip_not_found" {
		t.Errorf("code = %q, want trip_not_found", p.Code)
	}
	if p.Instance == nil || *p.Instance != "/api/v1/trips/42" {
		t.Errorf("instance = %v, want путь запроса", p.Instance)
	}
}

func TestError_Internal(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", nil)

	response.Error(rec, req, errors.New("pgx: connection refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", p.Code)
	}
}

func TestInvalidRequest(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", nil)

	response.InvalidRequest(rec, req, "invalid JSON body")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Code != "invalid_request" || p.Detail == nil || *p.Detail != "invalid JSON body" {
		t.Errorf("problem = %+v", p)
	}
}

// Ошибка 5xx передаётся middleware логирования, а ошибка клиента — нет:
// 4xx не поломка сервиса, её текст в лог запроса не нужен.
func TestError_ReportsOnlyInternalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantReport bool
	}{
		{name: "500 reported", err: errors.New("connection refused"), wantReport: true},
		{name: "404 not reported", err: trip.ErrTripNotFound, wantReport: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/x", nil)
			ctx, reported := requesterror.Attach(req.Context())

			response.Error(httptest.NewRecorder(), req.WithContext(ctx), tt.err)

			got := reported()
			if tt.wantReport && !errors.Is(got, tt.err) {
				t.Errorf("reported = %v, want %v", got, tt.err)
			}
			if !tt.wantReport && got != nil {
				t.Errorf("reported = %v, want nil", got)
			}
		})
	}
}
