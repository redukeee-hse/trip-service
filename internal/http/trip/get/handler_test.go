package get_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/trip/get"
)

type ctxKey struct{}

// serve выполняет запрос к ручке и возвращает записанный ответ.
func serve(t *testing.T, uc *MocktripGetter, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	// Метка в контексте: по ней проверяем, что в usecase ушёл контекст запроса,
	// а не context.Background() — иначе отмена запроса и трейс не дойдут до БД.
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "request-ctx"))
	rec := httptest.NewRecorder()
	get.New(uc).Handle(rec, req, id)
	return rec
}

func requestCtx() gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		ctx, ok := x.(context.Context)
		return ok && ctx.Value(ctxKey{}) == "request-ctx"
	})
}

func TestHandle_OK(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMocktripGetter(ctrl)

	id := uuid.New()
	finished := time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC)
	stored := trip.Trip{
		ID:         id,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 59.94, Longitude: 30.31},
		Price:      1450,
		Status:     trip.StatusCompleted,
		StartedAt:  time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		FinishedAt: &finished,
	}
	uc.EXPECT().Execute(requestCtx(), id).Return(stored, nil).Times(1)

	rec := serve(t, uc, id)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got api.Trip
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("тело не api.Trip: %v", err)
	}
	if got.Id != id || got.Status != "completed" || got.Price != 1450 ||
		got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Errorf("тело ответа %+v не соответствует поездке %+v", got, stored)
	}
}

func TestHandle_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			// Обязательный сценарий 5.
			name:       "не найдена → 404",
			err:        fmt.Errorf("get trip: %w", trip.ErrTripNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			// Обязательный сценарий 7.
			name:       "ошибка репозитория → 500",
			err:        fmt.Errorf("get trip: %w", errors.New(`pgx: SQLSTATE 57P01 terminating connection`)),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			uc := NewMocktripGetter(ctrl)
			id := uuid.New()
			uc.EXPECT().Execute(requestCtx(), id).Return(trip.Trip{}, tt.err).Times(1)

			rec := serve(t, uc, id)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
			body := rec.Body.String()
			for _, leak := range []string{"pgx", "SQLSTATE", "get trip"} {
				if strings.Contains(body, leak) {
					t.Errorf("в тело ответа утекло %q: %s", leak, body)
				}
			}
			var p api.Problem
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatalf("тело не problem+json: %v", err)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
			}
			if p.Instance == nil || *p.Instance != "/api/v1/trips/"+id.String() {
				t.Errorf("instance = %v, want путь запроса", p.Instance)
			}
		})
	}
}
