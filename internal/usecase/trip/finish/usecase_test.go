package finish_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/finish"
)

// clockNow — «текущее время» в часах usecase: не UTC и с наносекундами,
// чтобы проверить, что usecase приводит его к UTC и обрезает до микросекунд.
var clockNow = time.Date(2026, 11, 10, 15, 30, 0, 123456789, time.FixedZone("MSK", 3*60*60))

// wantFinishedAt — то, что должно уйти в репозиторий и в ответ.
var wantFinishedAt = time.Date(2026, 11, 10, 12, 30, 0, 123456000, time.UTC)

func fixedClock() time.Time { return clockNow }

// runInTx — мок txManager, который честно выполняет fn, как настоящая транзакция.
func runInTx(tx *MocktxManager) {
	tx.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		}).
		Times(1)
}

func activeTrip(id uuid.UUID) trip.Trip {
	return trip.Trip{
		ID:         id,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
	}
}

func TestUsecase_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	repo := NewMocktripRepository(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()
	stored := activeTrip(id)
	from := trip.StatusActive

	runInTx(tx)
	gomock.InOrder(
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(stored, nil),
		repo.EXPECT().Finish(gomock.Any(), id, wantFinishedAt).Return(nil),
		repo.EXPECT().AddStatusHistory(gomock.Any(), id, &from, trip.StatusCompleted).Return(nil),
	)
	// Счётчик tripgo.trips.completed — ровно один раз на успешное завершение.
	metrics.EXPECT().TripCompleted(gomock.Any()).Times(1)

	got, err := finish.New(tx, repo, fixedClock, metrics, slog.New(slog.DiscardHandler)).Execute(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if got.Status != trip.StatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, trip.StatusCompleted)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(wantFinishedAt) || got.FinishedAt.Location() != time.UTC {
		t.Errorf("FinishedAt = %v, want %v (UTC)", got.FinishedAt, wantFinishedAt)
	}
	// Остальные поля не должны потеряться по дороге.
	want := stored
	want.Status = trip.StatusCompleted
	want.FinishedAt = got.FinishedAt
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUsecase_Execute_Errors(t *testing.T) {
	t.Parallel()

	errDB := errors.New("connection refused")

	tests := []struct {
		name string
		// setup описывает ожидаемые вызовы. Вызов, который не описан, gomock
		// считает ошибкой — так проверяем, что после сбоя usecase останавливается.
		setup   func(tx *MocktxManager, repo *MocktripRepository, id uuid.UUID)
		wantErr error
	}{
		{
			name: "поездка уже завершена — ErrTripCompleted, ничего не пишем",
			setup: func(tx *MocktxManager, repo *MocktripRepository, id uuid.UUID) {
				runInTx(tx)
				done := activeTrip(id)
				done.Status = trip.StatusCompleted
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(done, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "поездка не найдена — ErrTripNotFound",
			setup: func(tx *MocktxManager, repo *MocktripRepository, id uuid.UUID) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "ошибка Finish — историю не пишем",
			setup: func(tx *MocktxManager, repo *MocktripRepository, id uuid.UUID) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
				repo.EXPECT().Finish(gomock.Any(), id, wantFinishedAt).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "ошибка AddStatusHistory — пробрасывается, чтобы транзакция откатилась",
			setup: func(tx *MocktxManager, repo *MocktripRepository, id uuid.UUID) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
				repo.EXPECT().Finish(gomock.Any(), id, wantFinishedAt).Return(nil)
				repo.EXPECT().AddStatusHistory(gomock.Any(), id, gomock.Any(), trip.StatusCompleted).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "транзакция не открылась — в репозиторий не ходим",
			setup: func(tx *MocktxManager, _ *MocktripRepository, _ uuid.UUID) {
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			tx := NewMocktxManager(ctrl)
			repo := NewMocktripRepository(ctrl)
			// Без EXPECT: при ошибке счётчик трогать нельзя, строгий мок это проверит.
			metrics := NewMocktripMetrics(ctrl)
			id := uuid.New()
			tt.setup(tx, repo, id)

			got, err := finish.New(tx, repo, fixedClock, metrics, slog.New(slog.DiscardHandler)).Execute(context.Background(), id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want errors.Is(%v)", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), id.String()) {
				t.Errorf("ошибка без контекста: %q не содержит id %s", err, id)
			}
			if got != (trip.Trip{}) {
				t.Errorf("при ошибке ожидалась пустая поездка, got %+v", got)
			}
		})
	}
}
