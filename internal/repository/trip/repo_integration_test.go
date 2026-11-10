//go:build integration

package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/infra/postgres/transaction"
	triprepo "github.com/redukeee-hse/avitoService/internal/repository/trip"
)

const queryTimeout = 5 * time.Second

var errBoom = errors.New("boom")

func setup(t *testing.T) (*triprepo.Repository, *transaction.Manager) {
	t.Helper()
	tx := transaction.New(testPool)
	return triprepo.New(tx, queryTimeout), tx
}

// newTrip возвращает валидную активную поездку и регистрирует её удаление.
// Время обрезано до микросекунд: столько хранит TIMESTAMPTZ.
func newTrip(t *testing.T, driverID uuid.UUID) trip.Trip {
	t.Helper()
	tr := trip.Trip{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		DriverID:   driverID,
		StartPoint: trip.Coordinates{Latitude: 55.751244, Longitude: 37.618423},
		EndPoint:   trip.Coordinates{Latitude: 59.93863, Longitude: 30.31413},
		Price:      1500,
		Status:     trip.StatusActive,
		StartedAt:  time.Now().UTC().Truncate(time.Microsecond),
	}
	t.Cleanup(func() {
		// ON DELETE CASCADE удаляет и trip_status_history.
		_, err := testPool.Exec(context.Background(), `DELETE FROM trips WHERE id = $1`, tr.ID)
		if err != nil {
			t.Errorf("cleanup trip %s: %v", tr.ID, err)
		}
	})
	return tr
}

func countTrips(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM trips WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count trips: %v", err)
	}
	return n
}

type historyRow struct {
	from *string
	to   string
}

func history(t *testing.T, tripID uuid.UUID) []historyRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT from_status, to_status FROM trip_status_history WHERE trip_id = $1 ORDER BY id`, tripID)
	if err != nil {
		t.Fatalf("select history: %v", err)
	}
	defer rows.Close()

	var res []historyRow
	for rows.Next() {
		var h historyRow
		if err := rows.Scan(&h.from, &h.to); err != nil {
			t.Fatalf("scan history: %v", err)
		}
		res = append(res, h)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate history: %v", err)
	}
	return res
}

func assertTripEqual(t *testing.T, got, want trip.Trip) {
	t.Helper()
	if got.ID != want.ID || got.UserID != want.UserID || got.DriverID != want.DriverID ||
		got.StartPoint != want.StartPoint || got.EndPoint != want.EndPoint ||
		got.Price != want.Price || got.Status != want.Status || !got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("trip mismatch:\n got  %+v\n want %+v", got, want)
	}
	// Equal сравнивает момент и не видит зону. Проверяем её отдельно: pgx отдаёт
	// timestamptz в time.Local, и без .UTC() в scanTrip ответ create (Z) и
	// get/finish (+03:00) расходились бы по формату.
	if got.StartedAt.Location() != time.UTC {
		t.Errorf("started_at в зоне %s, want UTC", got.StartedAt.Location())
	}
	if got.FinishedAt != nil && got.FinishedAt.Location() != time.UTC {
		t.Errorf("finished_at в зоне %s, want UTC", got.FinishedAt.Location())
	}
	switch {
	case got.FinishedAt == nil && want.FinishedAt == nil:
	case got.FinishedAt == nil || want.FinishedAt == nil:
		t.Fatalf("finished_at = %v, want %v", got.FinishedAt, want.FinishedAt)
	case !got.FinishedAt.Equal(*want.FinishedAt):
		t.Fatalf("finished_at = %v, want %v", *got.FinishedAt, *want.FinishedAt)
	}
}

func TestRepository_CreateAndGetByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	want := newTrip(t, uuid.New())

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	assertTripEqual(t, got, want)
}

func TestRepository_GetByID_NotFound(t *testing.T) {
	t.Parallel()
	repo, _ := setup(t)

	for name, get := range map[string]func(context.Context, uuid.UUID) (trip.Trip, error){
		"GetByID":          repo.GetByID,
		"GetByIDForUpdate": repo.GetByIDForUpdate,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := get(context.Background(), uuid.New())
			if !errors.Is(err, trip.ErrTripNotFound) {
				t.Fatalf("err = %v, want ErrTripNotFound", err)
			}
		})
	}
}

func TestRepository_Create_DriverBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	driverID := uuid.New()

	first := newTrip(t, driverID)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create first: %v", err)
	}

	second := newTrip(t, driverID)
	if err := repo.Create(ctx, second); !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("Create second: err = %v, want ErrDriverBusy", err)
	}

	// Индекс частичный (WHERE status = 'active'): после завершения водитель свободен.
	if err := repo.Finish(ctx, first.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create after finish: %v", err)
	}
}

func TestRepository_Create_DuplicateIDIsNotDriverBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)

	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Тот же id, другой водитель: это 23505 по trips_pkey, а не по
	// trips_driver_active_uniq — доменной ErrDriverBusy тут быть не должно.
	dup := tr
	dup.DriverID = uuid.New()
	err := repo.Create(ctx, dup)

	var pgErr *pgconn.PgError
	if errors.Is(err, trip.ErrDriverBusy) || !errors.As(err, &pgErr) || pgErr.ConstraintName != "trips_pkey" {
		t.Fatalf("err = %v, want wrapped pg error on trips_pkey", err)
	}
}

func TestRepository_Finish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	finishedAt := tr.StartedAt.Add(25 * time.Minute)
	if err := repo.Finish(ctx, tr.ID, finishedAt); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := repo.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	want := tr
	want.Status = trip.StatusCompleted
	want.FinishedAt = &finishedAt
	assertTripEqual(t, got, want)
}

func TestRepository_AddStatusHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, _ := setup(t)
	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	active := trip.StatusActive
	if err := repo.AddStatusHistory(ctx, tr.ID, nil, trip.StatusActive); err != nil {
		t.Fatalf("AddStatusHistory nil->active: %v", err)
	}
	if err := repo.AddStatusHistory(ctx, tr.ID, &active, trip.StatusCompleted); err != nil {
		t.Fatalf("AddStatusHistory active->completed: %v", err)
	}

	got := history(t, tr.ID)
	if len(got) != 2 {
		t.Fatalf("history len = %d, want 2", len(got))
	}
	if got[0].from != nil || got[0].to != "active" {
		t.Errorf("history[0] = %v -> %q, want NULL -> active", got[0].from, got[0].to)
	}
	if got[1].from == nil || *got[1].from != "active" || got[1].to != "completed" {
		t.Errorf("history[1] = %v -> %q, want active -> completed", got[1].from, got[1].to)
	}
}

// Обязательный сценарий ТЗ №8: INSERT в trips и trip_status_history
// внутри одной транзакции откатывается целиком.
func TestTransaction_RollbackBothTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// second выполняется после Create в той же транзакции и должен вернуть ошибку.
		second  func(ctx context.Context, repo *triprepo.Repository, tr trip.Trip) error
		wantErr func(err error) bool
	}{
		{
			name: "business error after both inserts",
			second: func(ctx context.Context, repo *triprepo.Repository, tr trip.Trip) error {
				if err := repo.AddStatusHistory(ctx, tr.ID, nil, trip.StatusActive); err != nil {
					return err
				}
				return errBoom
			},
			wantErr: func(err error) bool { return errors.Is(err, errBoom) },
		},
		{
			name: "database error on history insert",
			second: func(ctx context.Context, repo *triprepo.Repository, tr trip.Trip) error {
				// to_status нарушает CHECK — падает сама база на второй таблице.
				return repo.AddStatusHistory(ctx, tr.ID, nil, trip.Status("bogus"))
			},
			wantErr: func(err error) bool {
				var pgErr *pgconn.PgError
				return errors.As(err, &pgErr) && pgErr.Code == "23514"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repo, txm := setup(t)
			tr := newTrip(t, uuid.New())

			err := txm.Do(ctx, func(ctx context.Context) error {
				if err := repo.Create(ctx, tr); err != nil {
					return err
				}
				return tt.second(ctx, repo, tr)
			})
			if !tt.wantErr(err) {
				t.Fatalf("Do: unexpected err %v", err)
			}

			if n := countTrips(t, tr.ID); n != 0 {
				t.Errorf("trips rows = %d, want 0 (rolled back)", n)
			}
			if h := history(t, tr.ID); len(h) != 0 {
				t.Errorf("history rows = %d, want 0 (rolled back)", len(h))
			}
		})
	}
}

func TestTransaction_CommitBothTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, txm := setup(t)
	tr := newTrip(t, uuid.New())

	err := txm.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, tr); err != nil {
			return err
		}
		return repo.AddStatusHistory(ctx, tr.ID, nil, trip.StatusActive)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if n := countTrips(t, tr.ID); n != 1 {
		t.Errorf("trips rows = %d, want 1", n)
	}
	if h := history(t, tr.ID); len(h) != 1 {
		t.Errorf("history rows = %d, want 1", len(h))
	}
}

func TestTransaction_NestedDoJoinsOuter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, txm := setup(t)
	tr := newTrip(t, uuid.New())

	err := txm.Do(ctx, func(ctx context.Context) error {
		if err := txm.Do(ctx, func(ctx context.Context) error {
			return repo.Create(ctx, tr)
		}); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do: err = %v, want errBoom", err)
	}

	// Если бы вложенный Do открыл свою транзакцию и закоммитил её,
	// поездка осталась бы в базе.
	if n := countTrips(t, tr.ID); n != 0 {
		t.Errorf("trips rows = %d, want 0", n)
	}
}

func TestTransaction_DomainErrorIsNotWrapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, txm := setup(t)
	driverID := uuid.New()

	busy := newTrip(t, driverID)
	if err := repo.Create(ctx, busy); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := txm.Do(ctx, func(ctx context.Context) error {
		return repo.Create(ctx, newTrip(t, driverID))
	})
	if !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("Do: err = %v, want ErrDriverBusy through tx manager", err)
	}
}

func TestRepository_GetByIDForUpdate_LocksRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, txm := setup(t)
	tr := newTrip(t, uuid.New())
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := txm.Do(ctx, func(ctx context.Context) error {
		if _, err := repo.GetByIDForUpdate(ctx, tr.ID); err != nil {
			return err
		}
		// Отдельное соединение из пула, вне транзакции: NOWAIT сразу
		// падает с 55P03, если строка уже заблокирована. Без sleep.
		_, err := testPool.Exec(context.Background(),
			`SELECT 1 FROM trips WHERE id = $1 FOR UPDATE NOWAIT`, tr.ID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Errorf("concurrent lock: err = %v, want lock_not_available (55P03)", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}
