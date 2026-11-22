package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/http/domainerror"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
)

const codeInvalidRequest = "invalid_request"

func TestProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		// wantDetail — подстрока, которая должна быть в Detail.
		wantDetail string
	}{
		{
			name:       "не найдена → 404",
			err:        trip.ErrTripNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			// Usecase оборачивает ошибки через %w — маппер обязан видеть сквозь обёртку.
			name:       "не найдена, обёрнута usecase → всё равно 404",
			err:        fmt.Errorf("get trip 42: %w", trip.ErrTripNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name:       "водитель занят → 409",
			err:        fmt.Errorf("create trip: %w", trip.ErrDriverBusy),
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name:       "поездка завершена → 409",
			err:        fmt.Errorf("finish trip 42: %w", trip.ErrTripCompleted),
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
		},
		{
			name:       "конфликт Idempotency-Key → 409",
			err:        fmt.Errorf("create trip: %w", create.ErrIdempotencyConflict),
			wantStatus: http.StatusConflict,
			wantCode:   "idempotency_conflict",
		},
		{
			name:       "ошибка валидации → 400 с полем и причиной",
			err:        &trip.ValidationError{Field: "start_point.latitude", Reason: "must be in [-90, 90]"},
			wantStatus: http.StatusBadRequest,
			wantCode:   codeInvalidRequest,
			wantDetail: "start_point.latitude",
		},
		{
			name:       "ошибка валидации, обёрнута → errors.As находит",
			err:        fmt.Errorf("create trip: %w", &trip.ValidationError{Field: "price", Reason: "must be >= 0"}),
			wantStatus: http.StatusBadRequest,
			wantCode:   codeInvalidRequest,
			wantDetail: "price",
		},
		{
			name:       "неизвестная ошибка → 500",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := domainerror.Problem(tt.err)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if int(body.Status) != status {
				t.Errorf("body.Status = %d расходится со статусом ответа %d", body.Status, status)
			}
			if body.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", body.Code, tt.wantCode)
			}
			if want := "https://tripgo.example/problems/" + strings.ReplaceAll(tt.wantCode, "_", "-"); body.Type != want {
				t.Errorf("type = %q, want %q", body.Type, want)
			}
			if body.Title == "" || body.Detail == nil || *body.Detail == "" {
				t.Errorf("title и detail должны быть заполнены: %+v", body)
			}
			if tt.wantDetail != "" && !strings.Contains(*body.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want содержит %q", *body.Detail, tt.wantDetail)
			}
		})
	}
}

// Обязательный сценарий 7: ошибка репозитория → 500 и никаких деталей в теле.
func TestProblem_InternalErrorHidesDetails(t *testing.T) {
	t.Parallel()

	secret := `ERROR: relation "trips" does not exist (SQLSTATE 42P01) password=hunter2`
	err := fmt.Errorf("finish trip 42: %w", errors.New(secret))

	status, body := domainerror.Problem(err)

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	for _, field := range []string{body.Title, *body.Detail, body.Code, body.Type} {
		for _, leak := range []string{"SQLSTATE", "trips", "hunter2", "finish trip"} {
			if strings.Contains(field, leak) {
				t.Errorf("в ответ утекло %q: %q", leak, field)
			}
		}
	}
}

func TestInvalidRequest(t *testing.T) {
	t.Parallel()

	status, body := domainerror.InvalidRequest(`field "price" is required`)

	if status != http.StatusBadRequest || body.Status != http.StatusBadRequest {
		t.Errorf("status = %d / body %d, want 400", status, body.Status)
	}
	if body.Code != codeInvalidRequest {
		t.Errorf("code = %q, want invalid_request", body.Code)
	}
	if body.Detail == nil || *body.Detail != `field "price" is required` {
		t.Errorf("detail = %v, want текст как есть", body.Detail)
	}
}
