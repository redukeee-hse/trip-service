package listpositions_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/listpositions"
)

func TestExecute_Success(t *testing.T) {
	t.Parallel()

	repo := NewMocktripRepository(gomock.NewController(t))
	id := uuid.New()
	want := []trip.Position{
		{ID: 1, TripID: id, RecordedAt: time.Date(2026, 11, 27, 12, 1, 0, 0, time.UTC)},
		{ID: 2, TripID: id, RecordedAt: time.Date(2026, 11, 27, 12, 5, 0, 0, time.UTC)},
	}
	// Сначала проверка существования, потом точки: иначе 404 не отличить от пустого маршрута.
	gomock.InOrder(
		repo.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusActive}, nil),
		repo.EXPECT().ListPositions(gomock.Any(), id).Return(want, nil),
	)

	got, err := listpositions.New(repo).Execute(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Маршрут завершённой поездки тоже отдаём: читать историю можно всегда,
// запрет только на запись новых точек.
func TestExecute_CompletedTrip(t *testing.T) {
	t.Parallel()

	repo := NewMocktripRepository(gomock.NewController(t))
	id := uuid.New()
	repo.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: trip.StatusCompleted}, nil)
	repo.EXPECT().ListPositions(gomock.Any(), id).Return([]trip.Position{}, nil)

	if _, err := listpositions.New(repo).Execute(context.Background(), id); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestExecute_NotFound(t *testing.T) {
	t.Parallel()

	repo := NewMocktripRepository(gomock.NewController(t))
	id := uuid.New()
	// ListPositions не ожидается: на несуществующей поездке второй запрос не нужен.
	repo.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)

	_, err := listpositions.New(repo).Execute(context.Background(), id)
	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("err = %v, want ErrTripNotFound", err)
	}
}

func TestExecute_ListError(t *testing.T) {
	t.Parallel()

	repo := NewMocktripRepository(gomock.NewController(t))
	id := uuid.New()
	errDB := errors.New("db is down")
	repo.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id}, nil)
	repo.EXPECT().ListPositions(gomock.Any(), id).Return(nil, errDB)

	_, err := listpositions.New(repo).Execute(context.Background(), id)
	if !errors.Is(err, errDB) {
		t.Fatalf("err = %v, want errors.Is %v", err, errDB)
	}
}
