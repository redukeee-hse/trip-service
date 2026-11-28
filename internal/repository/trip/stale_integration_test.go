//go:build integration

package trip_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// Обязательный тест ЛР3 п. 7.1: в выборку попадают только активные поездки,
// молчащие дольше границы; свежие и завершённые — нет.
//
// Время поездок — 2000 год: другие тесты пакета создают поездки с
// started_at = now, граница в 2000 году их не захватит. Не параллельный:
// TestListStale_Keyset тоже работает со старыми датами.
func TestListStale(t *testing.T) {
	ctx := context.Background()
	repo, _ := setup(t)

	before := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)

	create := func(startedAt time.Time) trip.Trip {
		tr := newTrip(t, uuid.New())
		tr.StartedAt = startedAt
		if err := repo.Create(ctx, tr); err != nil {
			t.Fatalf("create trip: %v", err)
		}
		return tr
	}
	touch := func(tr trip.Trip, at time.Time) {
		if err := repo.TouchLastPosition(ctx, tr.ID, at); err != nil {
			t.Fatalf("touch: %v", err)
		}
	}

	// Попадают:
	noCoords := create(before.Add(-10 * time.Minute)) // координат не было → от started_at
	oldCoord := create(before.Add(-20 * time.Minute))
	touch(oldCoord, before.Add(-time.Minute)) // последняя точка минуту до границы

	// Не попадают:
	fresh := create(before.Add(-30 * time.Minute)) // давно начата, но точка свежая:
	touch(fresh, before.Add(time.Minute))          // started_at не должен «перебить» last_position_at
	completed := create(before.Add(-40 * time.Minute))
	if err := repo.Finish(ctx, completed.ID, before.Add(-35*time.Minute)); err != nil {
		t.Fatalf("finish: %v", err)
	}
	onBoundary := create(before) // ровно на границе — ещё не устарела (строго <)

	got, err := repo.ListStale(ctx, before, nil, 100)
	if err != nil {
		t.Fatalf("ListStale: %v", err)
	}

	// Самые давние первыми: noCoords (−10m) раньше oldCoord (−1m),
	// хотя oldCoord начата раньше — сортировка по COALESCE, а не по started_at.
	want := []trip.StaleTrip{
		{ID: noCoords.ID, DriverID: noCoords.DriverID, LastSeenAt: noCoords.StartedAt},
		{ID: oldCoord.ID, DriverID: oldCoord.DriverID, LastSeenAt: before.Add(-time.Minute)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d поездок %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].DriverID != want[i].DriverID || !got[i].LastSeenAt.Equal(want[i].LastSeenAt) {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, s := range got {
		switch s.ID {
		case fresh.ID:
			t.Error("в выборку попала поездка со свежей координатой")
		case completed.ID:
			t.Error("в выборку попала завершённая поездка")
		case onBoundary.ID:
			t.Error("в выборку попала поездка ровно на границе (нужно строго <)")
		}
	}
}

// Keyset-продолжение: у части поездок одинаковое время, порции по 2 режут
// их посередине. Склеенные порции должны дать ровно полную выборку — без
// пропусков и повторов.
func TestListStale_Keyset(t *testing.T) { // не параллельный, см. TestListStale
	ctx := context.Background()
	repo, _ := setup(t)

	before := time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC)
	same := before.Add(-10 * time.Minute)
	for _, at := range []time.Time{same, same, same, same, same.Add(time.Minute), same.Add(-time.Minute)} {
		tr := newTrip(t, uuid.New())
		tr.StartedAt = at
		if err := repo.Create(ctx, tr); err != nil {
			t.Fatalf("create trip: %v", err)
		}
	}

	all, err := repo.ListStale(ctx, before, nil, 100)
	if err != nil {
		t.Fatalf("ListStale: %v", err)
	}
	if len(all) != 6 {
		t.Fatalf("полная выборка %d, want 6", len(all))
	}

	var paged []trip.StaleTrip
	var after *trip.StaleCursor
	for range 10 { // страховка от бесконечного цикла
		page, err := repo.ListStale(ctx, before, after, 2)
		if err != nil {
			t.Fatalf("ListStale page: %v", err)
		}
		paged = append(paged, page...)
		if len(page) < 2 {
			break
		}
		c := page[len(page)-1].Cursor()
		after = &c
	}

	if len(paged) != len(all) {
		t.Fatalf("по порциям %d поездок, полная выборка %d: потеряли или задвоили", len(paged), len(all))
	}
	for i := range all {
		if paged[i].ID != all[i].ID {
			t.Errorf("[%d] по порциям %s, в полной выборке %s", i, paged[i].ID, all[i].ID)
		}
	}
}
