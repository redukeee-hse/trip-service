package create_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/create"
)

// clockNow — не UTC и с наносекундами: проверяем приведение к UTC и обрезку до микросекунд.
var clockNow = time.Date(2026, 11, 10, 15, 30, 0, 123456789, time.FixedZone("MSK", 3*60*60))

var wantStartedAt = time.Date(2026, 11, 10, 12, 30, 0, 123456000, time.UTC)

const wantTTL = 24 * time.Hour

func fixedClock() time.Time { return clockNow }

type mocks struct {
	tx      *MocktxManager
	trips   *MocktripRepository
	keys    *MockidempotencyRepository
	metrics *MocktripMetrics
}

func newMocks(t *testing.T) mocks {
	ctrl := gomock.NewController(t)
	return mocks{
		tx:      NewMocktxManager(ctrl),
		trips:   NewMocktripRepository(ctrl),
		keys:    NewMockidempotencyRepository(ctrl),
		metrics: NewMocktripMetrics(ctrl),
	}
}

func (m mocks) usecase() *create.Usecase {
	return create.New(m.tx, m.trips, m.keys, fixedClock, m.metrics, slog.New(slog.DiscardHandler))
}

// expectCreatedMetric — счётчик tripgo.trips.created увеличивается ровно раз.
// Там, где ожидание не задано, строгий мок gomock упадёт на любом вызове:
// так проверяется, что повтор и ошибки счётчик не трогают.
func (m mocks) expectCreatedMetric() {
	m.metrics.EXPECT().TripCreated(gomock.Any()).Times(1)
}

// runInTx — мок txManager, который честно выполняет fn, как настоящая транзакция.
func (m mocks) runInTx() {
	m.tx.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		}).
		Times(1)
}

// input — тело запроса, как его отдаст хендлер.
func input() trip.Trip {
	return trip.Trip{
		UserID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		DriverID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
	}
}

func ptr[T any](v T) *T { return &v }

// Обязательный сценарий 1: создание поездки — успех.
// Заодно проверяем, что серверные поля задаёт usecase, а не клиент.
func TestExecute_CreatesTrip(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	in := input()
	// Клиент пытается подсунуть свои серверные поля — usecase должен их перезаписать.
	in.ID = uuid.MustParse("99999999-9999-9999-9999-999999999999")
	in.Status = trip.StatusCompleted
	in.StartedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	in.FinishedAt = ptr(time.Date(2000, 1, 1, 1, 0, 0, 0, time.UTC))

	var saved trip.Trip
	m.runInTx()
	gomock.InOrder(
		m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, tr trip.Trip) error {
				saved = tr
				return nil
			}),
		m.trips.EXPECT().
			AddStatusHistory(gomock.Any(), gomock.Any(), (*trip.Status)(nil), trip.StatusActive).
			DoAndReturn(func(_ context.Context, id uuid.UUID, _ *trip.Status, _ trip.Status) error {
				if id != saved.ID {
					t.Errorf("история записана для %s, а поездка создана с %s", id, saved.ID)
				}
				return nil
			}),
	)

	m.expectCreatedMetric()

	got, created, err := m.usecase().Execute(context.Background(), in, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !created {
		t.Error("created = false, want true")
	}

	if saved.ID == uuid.Nil || saved.ID == in.ID {
		t.Errorf("ID = %s: должен генерироваться сервером", saved.ID)
	}
	if saved.Status != trip.StatusActive {
		t.Errorf("Status = %q, want %q", saved.Status, trip.StatusActive)
	}
	if !saved.StartedAt.Equal(wantStartedAt) || saved.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt = %v, want %v (UTC, из часов)", saved.StartedAt, wantStartedAt)
	}
	if saved.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil", saved.FinishedAt)
	}

	// Данные клиента дошли до репозитория без изменений.
	want := input()
	want.ID, want.Status, want.StartedAt = saved.ID, trip.StatusActive, saved.StartedAt
	if saved != want {
		t.Errorf("в репозиторий ушло %+v, want %+v", saved, want)
	}
	if got != saved {
		t.Errorf("вернули %+v, а сохранили %+v", got, saved)
	}
}

// С новым ключом: ключ занимается за той же поездкой, которую потом создаём.
func TestExecute_NewIdempotencyKey(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	key := uuid.New()

	var claimedFor uuid.UUID
	m.runInTx()
	gomock.InOrder(
		m.keys.EXPECT().
			ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).
			DoAndReturn(func(_ context.Context, _ uuid.UUID, _ string, tripID uuid.UUID, _ time.Duration) (bool, error) {
				claimedFor = tripID
				return true, nil
			}),
		m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, tr trip.Trip) error {
				if tr.ID != claimedFor {
					t.Errorf("ключ занят за %s, а создана поездка %s", claimedFor, tr.ID)
				}
				return nil
			}),
		m.trips.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil),
	)

	m.expectCreatedMetric()

	got, created, err := m.usecase().Execute(context.Background(), input(), &key)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !created || got.ID != claimedFor {
		t.Errorf("created = %v, ID = %s; want true, %s", created, got.ID, claimedFor)
	}
}

// Повтор с тем же ключом и тем же телом: отдаём старую поездку, новую не создаём.
func TestExecute_ReplaySameKey(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	key := uuid.New()
	existing := input()
	existing.ID = uuid.New()
	existing.Status = trip.StatusActive
	existing.StartedAt = time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)

	var data string
	m.runInTx()
	gomock.InOrder(
		m.keys.EXPECT().
			ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).
			DoAndReturn(func(_ context.Context, _ uuid.UUID, d string, _ uuid.UUID, _ time.Duration) (bool, error) {
				data = d
				return false, nil
			}),
		m.keys.EXPECT().GetIdempotencyKey(gomock.Any(), key).
			DoAndReturn(func(context.Context, uuid.UUID) (string, uuid.UUID, error) {
				return data, existing.ID, nil // тот же отпечаток тела
			}),
		m.trips.EXPECT().GetByID(gomock.Any(), existing.ID).Return(existing, nil),
	)
	// Create и AddStatusHistory не ожидаются: если usecase их вызовет, gomock уронит тест.

	got, created, err := m.usecase().Execute(context.Background(), input(), &key)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if created {
		t.Error("created = true, want false: хендлер должен ответить 200, а не 201")
	}
	if got != existing {
		t.Errorf("got %+v, want ранее созданную %+v", got, existing)
	}
}

// Тот же ключ, другое тело → ErrIdempotencyConflict, ничего не создаём.
func TestExecute_IdempotencyConflict(t *testing.T) {
	t.Parallel()

	m := newMocks(t)
	key := uuid.New()

	m.runInTx()
	m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).Return(false, nil)
	m.keys.EXPECT().GetIdempotencyKey(gomock.Any(), key).Return("тело первого запроса", uuid.New(), nil)

	got, created, err := m.usecase().Execute(context.Background(), input(), &key)
	if !errors.Is(err, create.ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
	if created || got != (trip.Trip{}) {
		t.Errorf("при ошибке ожидались пустая поездка и created=false, got %+v, %v", got, created)
	}
}

// Отпечаток тела (requestData) учитывает каждое поле запроса и не зависит от
// серверных полей. Иначе повтор с другой ценой молча вернёт старую поездку.
func TestExecute_RequestFingerprint(t *testing.T) {
	t.Parallel()

	// claimData прогоняет Execute и возвращает строку, ушедшую в ClaimIdempotencyKey.
	claimData := func(t *testing.T, in trip.Trip) string {
		t.Helper()
		m := newMocks(t)
		key := uuid.New()
		var data string
		m.runInTx()
		m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).
			DoAndReturn(func(_ context.Context, _ uuid.UUID, d string, _ uuid.UUID, _ time.Duration) (bool, error) {
				data = d
				return true, nil
			})
		m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		m.trips.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
		m.expectCreatedMetric()
		if _, _, err := m.usecase().Execute(context.Background(), in, &key); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		return data
	}

	base := claimData(t, input())

	t.Run("то же тело — тот же отпечаток", func(t *testing.T) {
		t.Parallel()
		// ID и StartedAt генерируются заново, но отпечаток от них зависеть не должен.
		if got := claimData(t, input()); got != base {
			t.Errorf("отпечаток %q != %q для одинаковых тел", got, base)
		}
	})

	mutations := map[string]func(*trip.Trip){
		"user_id":               func(tr *trip.Trip) { tr.UserID = uuid.New() },
		"driver_id":             func(tr *trip.Trip) { tr.DriverID = uuid.New() },
		"start_point.latitude":  func(tr *trip.Trip) { tr.StartPoint.Latitude++ },
		"start_point.longitude": func(tr *trip.Trip) { tr.StartPoint.Longitude++ },
		"end_point.latitude":    func(tr *trip.Trip) { tr.EndPoint.Latitude++ },
		"end_point.longitude":   func(tr *trip.Trip) { tr.EndPoint.Longitude++ },
		"price":                 func(tr *trip.Trip) { tr.Price++ },
	}
	for field, mutate := range mutations {
		t.Run("другое "+field+" — другой отпечаток", func(t *testing.T) {
			t.Parallel()
			in := input()
			mutate(&in)
			if got := claimData(t, in); got == base {
				t.Errorf("поле %s не попало в отпечаток: %q", field, got)
			}
		})
	}
}

// Обязательный сценарий 6 (часть usecase): невалидное тело отклоняется
// до транзакции — tx и репозитории не вызываются вовсе.
func TestExecute_ValidationError(t *testing.T) {
	t.Parallel()

	m := newMocks(t) // ни одного EXPECT: любой вызов зависимостей — провал теста
	in := input()
	in.StartPoint.Latitude = 91
	key := uuid.New()

	got, created, err := m.usecase().Execute(context.Background(), in, &key)

	var vErr *trip.ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("err = %v, want *trip.ValidationError", err)
	}
	if vErr.Field != "start_point.latitude" {
		t.Errorf("Field = %q, want start_point.latitude", vErr.Field)
	}
	if created || got != (trip.Trip{}) {
		t.Errorf("при ошибке ожидались пустая поездка и created=false, got %+v, %v", got, created)
	}
}

// Сбои зависимостей: ошибка доходит до вызывающего (errors.Is), выполнение
// останавливается на упавшем шаге, наружу не утекает частичный результат.
func TestExecute_DependencyErrors(t *testing.T) {
	t.Parallel()

	errDB := errors.New("connection refused")

	tests := []struct {
		name    string
		withKey bool
		setup   func(m mocks, key uuid.UUID)
		wantErr error
	}{
		{
			// Обязательный сценарий 2: репозиторий ловит уникальный индекс и
			// возвращает доменную ошибку, usecase пропускает её наверх → 409.
			name: "водитель занят — ErrDriverBusy, историю не пишем",
			setup: func(m mocks, _ uuid.UUID) {
				m.runInTx()
				m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.ErrDriverBusy)
			},
			wantErr: trip.ErrDriverBusy,
		},
		{
			name: "ошибка AddStatusHistory — пробрасывается, чтобы транзакция откатилась",
			setup: func(m mocks, _ uuid.UUID) {
				m.runInTx()
				m.trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				m.trips.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "транзакция не открылась — в репозитории не ходим",
			setup: func(m mocks, _ uuid.UUID) {
				m.tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name:    "ошибка ClaimIdempotencyKey — поездку не создаём",
			withKey: true,
			setup: func(m mocks, key uuid.UUID) {
				m.runInTx()
				m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).Return(false, errDB)
			},
			wantErr: errDB,
		},
		{
			name:    "ошибка GetIdempotencyKey",
			withKey: true,
			setup: func(m mocks, key uuid.UUID) {
				m.runInTx()
				m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).Return(false, nil)
				m.keys.EXPECT().GetIdempotencyKey(gomock.Any(), key).Return("", uuid.Nil, errDB)
			},
			wantErr: errDB,
		},
		{
			name:    "ошибка GetByID при повторе",
			withKey: true,
			setup: func(m mocks, key uuid.UUID) {
				m.runInTx()
				var data string
				m.keys.EXPECT().ClaimIdempotencyKey(gomock.Any(), key, gomock.Any(), gomock.Any(), wantTTL).
					DoAndReturn(func(_ context.Context, _ uuid.UUID, d string, _ uuid.UUID, _ time.Duration) (bool, error) {
						data = d
						return false, nil
					})
				existingID := uuid.New()
				m.keys.EXPECT().GetIdempotencyKey(gomock.Any(), key).
					DoAndReturn(func(context.Context, uuid.UUID) (string, uuid.UUID, error) {
						return data, existingID, nil
					})
				m.trips.EXPECT().GetByID(gomock.Any(), existingID).Return(trip.Trip{}, errDB)
			},
			wantErr: errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newMocks(t)
			var key *uuid.UUID
			if tt.withKey {
				key = ptr(uuid.New())
				tt.setup(m, *key)
			} else {
				tt.setup(m, uuid.Nil)
			}

			got, created, err := m.usecase().Execute(context.Background(), input(), key)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want errors.Is(%v)", err, tt.wantErr)
			}
			if created || got != (trip.Trip{}) {
				t.Errorf("при ошибке ожидались пустая поездка и created=false, got %+v, %v", got, created)
			}
		})
	}
}
