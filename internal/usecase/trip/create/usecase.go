package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type Usecase struct {
	tx      txManager
	repo    tripRepository
	idempo  idempotencyRepository
	now     func() time.Time
	metrics tripMetrics
	log     *slog.Logger
}

func New(
	tx txManager,
	repo tripRepository,
	idempo idempotencyRepository,
	now func() time.Time,
	metrics tripMetrics,
	log *slog.Logger,
) *Usecase {
	return &Usecase{tx: tx, repo: repo, idempo: idempo, now: now, metrics: metrics, log: log}
}

// Cколько живёт Idempotency-Key.
const idempotencyKeyTTL = 24 * time.Hour

func (u *Usecase) Execute(ctx context.Context, t trip.Trip, idempotencyKey *uuid.UUID) (trip.Trip, bool, error) {
	if err := t.Validate(); err != nil {
		return trip.Trip{}, false, err
	}

	t.ID = uuid.New()
	t.Status = trip.StatusActive
	t.StartedAt = u.now().UTC().Truncate(time.Microsecond)
	t.FinishedAt = nil

	result := t
	created := true

	err := u.tx.Do(ctx, func(ctx context.Context) error {
		if idempotencyKey != nil {
			existing, replayed, err := u.claimKey(ctx, *idempotencyKey, t)
			if err != nil {
				return err
			}
			if replayed {
				result = existing
				created = false
				return nil
			}
		}
		if err := u.repo.Create(ctx, t); err != nil {
			return err
		}
		return u.repo.AddStatusHistory(ctx, t.ID, nil, trip.StatusActive)
	})
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("create trip: %w", err)
	}
	// Бизнес-событие логируем здесь, а не в middleware: только usecase знает,
	// что поездка действительно создана, а не возвращена повтором по ключу.
	// user_id и driver_id не пишем: это идентификаторы людей.
	// Повтор по Idempotency-Key — не новая поездка: ни лога, ни счётчика.
	if created {
		u.metrics.TripCreated(ctx)
		u.log.InfoContext(ctx, "trip created", slog.String("trip_id", result.ID.String()))
	}
	return result, created, nil
}

// claimKey занимает ключ за новой поездкой. Если ключ уже занят, возвращает ранее
// созданную поездку (replayed = true) или ErrIdempotencyConflict, если данные другие.
func (u *Usecase) claimKey(ctx context.Context, key uuid.UUID, t trip.Trip) (trip.Trip, bool, error) {
	data := requestData(t)
	claimed, err := u.idempo.ClaimIdempotencyKey(ctx, key, data, t.ID, idempotencyKeyTTL)
	if err != nil {
		return trip.Trip{}, false, err
	}
	if claimed {
		return trip.Trip{}, false, nil
	}

	storedData, tripID, err := u.idempo.GetIdempotencyKey(ctx, key)
	if err != nil {
		return trip.Trip{}, false, err
	}
	if storedData != data {
		return trip.Trip{}, false, ErrIdempotencyConflict
	}

	existing, err := u.repo.GetByID(ctx, tripID)
	if err != nil {
		return trip.Trip{}, false, err
	}
	return existing, true, nil
}

// requestData — данные запроса одной строкой, чтобы сравнить повтор с первым запросом.
func requestData(t trip.Trip) string {
	return fmt.Sprintf("%s|%s|%v|%v|%v|%v|%d",
		t.UserID, t.DriverID,
		t.StartPoint.Latitude, t.StartPoint.Longitude,
		t.EndPoint.Latitude, t.EndPoint.Longitude,
		t.Price,
	)
}
