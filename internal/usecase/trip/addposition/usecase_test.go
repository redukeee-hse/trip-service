package addposition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/addposition"
)

var (
	coords     = trip.Coordinates{Latitude: 59.9343, Longitude: 30.3351}
	recordedAt = time.Date(2026, 11, 27, 10, 5, 0, 0, time.UTC)
)

// fixedNow — «сейчас» в часах usecase: на 10 секунд позже recordedAt.
func fixedNow() time.Time { return recordedAt.Add(10 * time.Second) }

// runInTx — мок txManager, который честно выполняет fn, как настоящая транзакция.
func runInTx(tx *MocktxManager) {
	tx.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		}).
		Times(1)
}

func newPos(tripID uuid.UUID) trip.Position {
	return trip.Position{TripID: tripID, Coordinates: coords, RecordedAt: recordedAt}
}

func TestExecute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	repo := NewMocktripRepository(ctrl)

	id := uuid.New()
	saved := newPos(id)
	saved.ID = 42

	runInTx(tx)
	// Порядок важен: сначала блокировка строки поездки, потом проверка
	// статуса, потом записи. Блокировка — именно ForUpdate, иначе
	// параллельный finish проскочит между проверкой и INSERT.
	gomock.InOrder(
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil),
		repo.EXPECT().AddPosition(gomock.Any(), newPos(id)).Return(saved, nil),
		repo.EXPECT().TouchLastPosition(gomock.Any(), id, recordedAt).Return(nil),
	)

	got, err := addposition.New(tx, repo, fixedNow).Execute(context.Background(), id, coords, recordedAt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != saved {
		t.Errorf("got %+v, want %+v (с id из БД)", got, saved)
	}
}

// Невалидная точка отсекается до транзакции: ни соединения, ни блокировки.
func TestExecute_InvalidPosition_NoTx(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	repo := NewMocktripRepository(ctrl)
	// Никаких EXPECT: любой вызов tx или repo провалит тест.

	_, err := addposition.New(tx, repo, fixedNow).Execute(context.Background(), uuid.New(),
		trip.Coordinates{Latitude: 91, Longitude: 0}, recordedAt)

	var vErr *trip.ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
}

// «Сейчас» берётся из часов usecase, а не из time.Now(): точка на 2 минуты
// впереди fixedNow отвергается до транзакции.
func TestExecute_FuturePosition_NoTx(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	_, err := addposition.New(NewMocktxManager(ctrl), NewMocktripRepository(ctrl), fixedNow).
		Execute(context.Background(), uuid.New(), coords, fixedNow().Add(2*time.Minute))

	var vErr *trip.ValidationError
	if !errors.As(err, &vErr) || vErr.Field != "recorded_at" {
		t.Fatalf("err = %v, want *ValidationError по recorded_at", err)
	}
}

func TestExecute_Errors(t *testing.T) {
	t.Parallel()

	errDB := errors.New("db is down")

	tests := []struct {
		name  string
		setup func(repo *MocktripRepository, id uuid.UUID)
		want  error
	}{
		{
			name: "поездки нет → ErrTripNotFound, ничего не пишем",
			setup: func(repo *MocktripRepository, id uuid.UUID) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			want: trip.ErrTripNotFound,
		},
		{
			name: "поездка завершена → ErrTripCompleted, ничего не пишем",
			setup: func(repo *MocktripRepository, id uuid.UUID) {
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).
					Return(trip.Trip{ID: id, Status: trip.StatusCompleted}, nil)
			},
			want: trip.ErrTripCompleted,
		},
		{
			name: "INSERT упал → last_position_at не трогаем",
			setup: func(repo *MocktripRepository, id uuid.UUID) {
				gomock.InOrder(
					repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil),
					repo.EXPECT().AddPosition(gomock.Any(), gomock.Any()).Return(trip.Position{}, errDB),
				)
			},
			want: errDB,
		},
		{
			name: "UPDATE упал → ошибка наружу (транзакция откатит INSERT)",
			setup: func(repo *MocktripRepository, id uuid.UUID) {
				gomock.InOrder(
					repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil),
					repo.EXPECT().AddPosition(gomock.Any(), gomock.Any()).Return(newPos(id), nil),
					repo.EXPECT().TouchLastPosition(gomock.Any(), id, recordedAt).Return(errDB),
				)
			},
			want: errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			tx := NewMocktxManager(ctrl)
			repo := NewMocktripRepository(ctrl)
			id := uuid.New()

			runInTx(tx)
			tt.setup(repo, id)

			got, err := addposition.New(tx, repo, fixedNow).Execute(context.Background(), id, coords, recordedAt)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want errors.Is %v", err, tt.want)
			}
			if got != (trip.Position{}) {
				t.Errorf("при ошибке вернулась точка %+v, want нулевую", got)
			}
		})
	}
}
