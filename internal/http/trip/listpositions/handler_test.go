package listpositions_test

import (
	"encoding/json"
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
	"github.com/redukeee-hse/avitoService/internal/http/trip/listpositions"
)

func serve(t *testing.T, uc *MockpositionLister, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil)
	rec := httptest.NewRecorder()
	listpositions.New(uc).Handle(rec, req, id)
	return rec
}

func TestHandle_OK(t *testing.T) {
	t.Parallel()

	uc := NewMockpositionLister(gomock.NewController(t))
	id := uuid.New()
	at := time.Date(2026, 11, 27, 12, 1, 0, 0, time.UTC)
	uc.EXPECT().Execute(gomock.Any(), id).Return([]trip.Position{
		{ID: 1, TripID: id, Coordinates: trip.Coordinates{Latitude: 59.9, Longitude: 30.3}, RecordedAt: at},
		{ID: 2, TripID: id, Coordinates: trip.Coordinates{Latitude: 59.8, Longitude: 30.2}, RecordedAt: at.Add(time.Minute)},
	}, nil)

	rec := serve(t, uc, id)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var got api.TripPositionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("тело не TripPositionsResponse: %v", err)
	}
	// Порядок из usecase сохраняется как есть: сортирует SQL, не ручка.
	if len(got.Positions) != 2 || got.Positions[0].Id != 1 || got.Positions[1].Id != 2 ||
		got.Positions[0].TripId != id || !got.Positions[0].RecordedAt.Equal(at) {
		t.Errorf("тело %+v, want две точки поездки %s по порядку", got, id)
	}
}

// Пустой маршрут — [] в JSON, а не null: контракт обещает массив.
func TestHandle_EmptyIsArray(t *testing.T) {
	t.Parallel()

	uc := NewMockpositionLister(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(nil, nil)

	rec := serve(t, uc, uuid.New())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"positions":[]}` {
		t.Errorf("body = %s, want {\"positions\":[]}", body)
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
		{"нет поездки → 404", fmt.Errorf("list: %w", trip.ErrTripNotFound), http.StatusNotFound, "trip_not_found"},
		{"ошибка БД → 500", fmt.Errorf("list: %w", fmt.Errorf("pgx: SQLSTATE 08006")), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc := NewMockpositionLister(gomock.NewController(t))
			uc.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(nil, tt.err)

			rec := serve(t, uc, uuid.New())

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if strings.Contains(rec.Body.String(), "SQLSTATE") {
				t.Errorf("в тело утекла ошибка БД: %s", rec.Body)
			}
			var p api.Problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("тело не problem+json: %v", err)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
			}
		})
	}
}
