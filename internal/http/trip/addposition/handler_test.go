package addposition_test

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
	"github.com/redukeee-hse/avitoService/internal/http/trip/addposition"
)

func serve(t *testing.T, uc *MockpositionAdder, id uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	addposition.New(uc).Handle(rec, req, id)
	return rec
}

const validBody = `{"coordinates":{"latitude":59.9343,"longitude":30.3351},"recorded_at":"2026-11-27T10:05:00Z"}`

func TestHandle_Created(t *testing.T) {
	t.Parallel()

	uc := NewMockpositionAdder(gomock.NewController(t))
	id := uuid.New()
	at := time.Date(2026, 11, 27, 10, 5, 0, 0, time.UTC)
	coords := trip.Coordinates{Latitude: 59.9343, Longitude: 30.3351}
	uc.EXPECT().Execute(gomock.Any(), id, coords, at).
		Return(trip.Position{ID: 7, TripID: id, Coordinates: coords, RecordedAt: at}, nil)

	rec := serve(t, uc, id, validBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	var got api.TripPosition
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("тело не api.TripPosition: %v", err)
	}
	if got.Id != 7 || got.TripId != id || !got.RecordedAt.Equal(at) ||
		got.Coordinates.Latitude != 59.9343 || got.Coordinates.Longitude != 30.3351 {
		t.Errorf("тело %+v, want точку 7 поездки %s", got, id)
	}
}

// Форма тела проверяется до usecase: пропущенное поле не должно стать
// нулём (точка 0,0 в Гвинейском заливе или нулевое время).
func TestHandle_BadBody(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"не JSON":         `{`,
		"нет coordinates": `{"recorded_at":"2026-11-27T10:05:00Z"}`,
		"нет recorded_at": `{"coordinates":{"latitude":1,"longitude":2}}`,
		"нет latitude":    `{"coordinates":{"longitude":2},"recorded_at":"2026-11-27T10:05:00Z"}`,
		"null longitude":  `{"coordinates":{"latitude":1,"longitude":null},"recorded_at":"2026-11-27T10:05:00Z"}`,
		"лишнее поле":     `{"coordinates":{"latitude":1,"longitude":2},"recorded_at":"2026-11-27T10:05:00Z","speed":5}`,
		"кривое время":    `{"coordinates":{"latitude":1,"longitude":2},"recorded_at":"вчера"}`,
		"широта строкой":  `{"coordinates":{"latitude":"1","longitude":2},"recorded_at":"2026-11-27T10:05:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			uc := NewMockpositionAdder(gomock.NewController(t)) // без EXPECT: usecase не вызывается
			rec := serve(t, uc, uuid.New(), body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestHandle_UsecaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			"вне диапазона → 400", &trip.ValidationError{Field: "coordinates.latitude", Reason: "must be in [-90, 90]"},
			http.StatusBadRequest, "invalid_request",
		},
		{
			"нет поездки → 404", fmt.Errorf("add position: %w", trip.ErrTripNotFound),
			http.StatusNotFound, "trip_not_found",
		},
		{
			"завершена → 409", fmt.Errorf("add position: %w", trip.ErrTripCompleted),
			http.StatusConflict, "trip_completed",
		},
		{
			"ошибка БД → 500", fmt.Errorf("add position: %w", fmt.Errorf("pgx: SQLSTATE 08006")),
			http.StatusInternalServerError, "internal_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc := NewMockpositionAdder(gomock.NewController(t))
			uc.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(trip.Position{}, tt.err)

			rec := serve(t, uc, uuid.New(), validBody)

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
