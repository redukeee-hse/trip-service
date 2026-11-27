//go:build integration

package trip_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

func lastPositionAt(t *testing.T, tripID uuid.UUID) *time.Time {
	t.Helper()
	var at *time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT last_position_at FROM trips WHERE id = $1`, tripID).Scan(&at); err != nil {
		t.Fatalf("select last_position_at: %v", err)
	}
	return at
}

func TestAddPosition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)

	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create trip: %v", err)
	}

	at := time.Date(2026, 11, 27, 10, 5, 0, 123456000, time.UTC)
	in := trip.Position{TripID: tr.ID, Coordinates: trip.Coordinates{Latitude: 59.9343, Longitude: 30.3351}, RecordedAt: at}

	first, err := repo.AddPosition(ctx, in)
	if err != nil {
		t.Fatalf("AddPosition: %v", err)
	}
	second, err := repo.AddPosition(ctx, in)
	if err != nil {
		t.Fatalf("AddPosition: %v", err)
	}
	if first.ID == 0 || second.ID <= first.ID {
		t.Errorf("ids = %d, %d: want id из БД, растущие", first.ID, second.ID)
	}

	var lat, lon float64
	var got time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT latitude, longitude, recorded_at FROM trip_positions WHERE id = $1`, first.ID).
		Scan(&lat, &lon, &got); err != nil {
		t.Fatalf("select position: %v", err)
	}
	if lat != 59.9343 || lon != 30.3351 || !got.Equal(at) {
		t.Errorf("в БД (%v, %v, %v), want (59.9343, 30.3351, %v)", lat, lon, got, at)
	}
}

// Главный тест задачи: last_position_at только растёт, а первая точка
// записывается в NULL (ловушка «NULL < x — это не true»).
func TestTouchLastPosition_OnlyMovesForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)

	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create trip: %v", err)
	}
	if got := lastPositionAt(t, tr.ID); got != nil {
		t.Fatalf("у новой поездки last_position_at = %v, want NULL", got)
	}

	t1205 := time.Date(2026, 11, 27, 12, 5, 0, 0, time.UTC)
	t1201 := time.Date(2026, 11, 27, 12, 1, 0, 0, time.UTC)
	t1210 := time.Date(2026, 11, 27, 12, 10, 0, 0, time.UTC)

	steps := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"первая точка: NULL → 12:05", t1205, t1205},
		{"опоздавшая 12:01 не откатывает назад", t1201, t1205},
		{"повтор 12:05 ничего не ломает", t1205, t1205},
		{"свежая 12:10 сдвигает вперёд", t1210, t1210},
	}
	for _, s := range steps {
		if err := repo.TouchLastPosition(ctx, tr.ID, s.at); err != nil {
			t.Fatalf("%s: TouchLastPosition: %v", s.name, err)
		}
		got := lastPositionAt(t, tr.ID)
		if got == nil || !got.Equal(s.want) {
			t.Fatalf("%s: last_position_at = %v, want %v", s.name, got, s.want)
		}
	}
}

// Несуществующая поездка — не ошибка репозитория: 0 строк обновлено.
// Существование проверяет use case через GetByIDForUpdate.
func TestTouchLastPosition_UnknownTrip(t *testing.T) {
	t.Parallel()
	repo, _ := setup(t)

	if err := repo.TouchLastPosition(context.Background(), uuid.New(), time.Now().UTC()); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
