package create_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/trip/create"
	createuc "github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
)

type ctxKey struct{}

var (
	userID   = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	driverID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

const detailInvalidJSON = "invalid JSON body"

const validBody = `{
	"user_id": "11111111-1111-1111-1111-111111111111",
	"driver_id": "22222222-2222-2222-2222-222222222222",
	"start_point": {"latitude": 55.75, "longitude": 37.61},
	"end_point": {"latitude": 59.94, "longitude": 30.31},
	"price": 1450
}`

// wantInput — во что хендлер должен превратить validBody перед usecase.
var wantInput = trip.Trip{
	UserID:     userID,
	DriverID:   driverID,
	StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
	EndPoint:   trip.Coordinates{Latitude: 59.94, Longitude: 30.31},
	Price:      1450,
}

func created(id uuid.UUID) trip.Trip {
	t := wantInput
	t.ID = id
	t.Status = trip.StatusActive
	t.StartedAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	return t
}

func serve(t *testing.T, uc *MocktripCreator, body io.Reader, params api.CreateTripParams) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", body)
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "request-ctx"))
	rec := httptest.NewRecorder()
	create.New(uc).Handle(rec, req, params)
	return rec
}

// requestCtx — в usecase ушёл контекст запроса, а не context.Background().
func requestCtx() gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		ctx, ok := x.(context.Context)
		return ok && ctx.Value(ctxKey{}) == "request-ctx"
	})
}

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

// Обязательный сценарий 1 на уровне HTTP: 201, Location, тело — созданная поездка.
// Без заголовка Idempotency-Key в usecase уходит nil.
func TestHandle_Created(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMocktripCreator(ctrl)
	id := uuid.New()
	uc.EXPECT().
		Execute(requestCtx(), wantInput, gomock.Nil()).
		Return(created(id), true, nil).
		Times(1)

	rec := serve(t, uc, strings.NewReader(validBody), api.CreateTripParams{})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/trips/"+id.String() {
		t.Errorf("Location = %q, want /api/v1/trips/%s", loc, id)
	}
	var got api.Trip
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("тело не api.Trip: %v", err)
	}
	if got.Id != id || got.Status != "active" || got.UserId != userID || got.Price != 1450 {
		t.Errorf("тело %+v не соответствует созданной поездке", got)
	}
}

// Повтор с тем же ключом: usecase вернул created=false → 200, Location тот же.
func TestHandle_ReplayReturns200(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMocktripCreator(ctrl)
	id := uuid.New()
	key := uuid.New()
	uc.EXPECT().
		Execute(requestCtx(), wantInput, &key). // ключ из заголовка доходит до usecase как есть
		Return(created(id), false, nil).
		Times(1)

	rec := serve(t, uc, strings.NewReader(validBody), api.CreateTripParams{IdempotencyKey: &key})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/trips/"+id.String() {
		t.Errorf("Location = %q", loc)
	}
}

// Обязательный сценарий 6: форму тела проверяет транспорт, до usecase запрос не доходит.
// Мок без EXPECT: любой вызов Execute уронит тест.
func TestHandle_InvalidBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       io.Reader
		wantDetail string
	}{
		{
			name:       "нет обязательного поля",
			body:       strings.NewReader(strings.Replace(validBody, `"price": 1450`, `"price": null`, 1)),
			wantDetail: `field "price" is required`,
		},
		{
			name:       "лишнее поле",
			body:       strings.NewReader(strings.Replace(validBody, `"price": 1450`, `"price": 1450, "status": "completed"`, 1)),
			wantDetail: detailInvalidJSON,
		},
		{
			name:       "не UUID в user_id",
			body:       strings.NewReader(strings.Replace(validBody, `11111111-1111-1111-1111-111111111111`, `not-a-uuid`, 1)),
			wantDetail: detailInvalidJSON,
		},
		{
			name:       "строка вместо числа",
			body:       strings.NewReader(strings.Replace(validBody, `"price": 1450`, `"price": "1450"`, 1)),
			wantDetail: detailInvalidJSON,
		},
		{
			name:       "битый JSON",
			body:       strings.NewReader(`{"user_id":`),
			wantDetail: "expected a JSON object",
		},
		{
			name:       "тело не читается",
			body:       iotest.ErrReader(errors.New("connection reset")),
			wantDetail: "cannot read request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc := NewMocktripCreator(gomock.NewController(t))

			rec := serve(t, uc, tt.body, api.CreateTripParams{})

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			p := decodeProblem(t, rec)
			if p.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", p.Code)
			}
			if p.Detail == nil || *p.Detail != tt.wantDetail {
				t.Errorf("detail = %v, want %q", p.Detail, tt.wantDetail)
			}
		})
	}
}

// Ошибки из usecase хендлер не разбирает, а отдаёт в domainerror.
func TestHandle_UsecaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			// Обязательный сценарий 6, правила поездки: координата вне диапазона.
			name:       "ValidationError → 400",
			err:        &trip.ValidationError{Field: "start_point.latitude", Reason: "must be in [-90, 90]"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			// Обязательный сценарий 2.
			name:       "водитель занят → 409",
			err:        fmt.Errorf("create trip: %w", trip.ErrDriverBusy),
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name:       "конфликт Idempotency-Key → 409",
			err:        fmt.Errorf("create trip: %w", createuc.ErrIdempotencyConflict),
			wantStatus: http.StatusConflict,
			wantCode:   "idempotency_conflict",
		},
		{
			// Обязательный сценарий 7.
			name:       "ошибка БД → 500",
			err:        fmt.Errorf("create trip: %w", errors.New("pgx: SQLSTATE 23502 null value")),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc := NewMocktripCreator(gomock.NewController(t))
			uc.EXPECT().Execute(requestCtx(), wantInput, gomock.Nil()).Return(trip.Trip{}, false, tt.err).Times(1)

			rec := serve(t, uc, strings.NewReader(validBody), api.CreateTripParams{})

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("Location = %q при ошибке, want пусто", loc)
			}
			body := rec.Body.String()
			for _, leak := range []string{"pgx", "SQLSTATE", "create trip"} {
				if strings.Contains(body, leak) {
					t.Errorf("в тело утекло %q: %s", leak, body)
				}
			}
			var p api.Problem
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatalf("тело не problem+json: %v", err)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
			}
		})
	}
}
