// Package addposition — сохранение координаты активной поездки.
package addposition

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type Usecase struct {
	tx   txManager
	repo tripRepository
	now  func() time.Time
}

func New(tx txManager, repo tripRepository, now func() time.Time) *Usecase {
	return &Usecase{tx: tx, repo: repo, now: now}
}

// Execute сохраняет точку и сдвигает last_position_at поездки.
//
// Ошибки: *trip.ValidationError (400), trip.ErrTripNotFound (404),
// trip.ErrTripCompleted (409), остальное — 500.
func (u *Usecase) Execute(
	ctx context.Context,
	tripID uuid.UUID,
	coords trip.Coordinates,
	recordedAt time.Time,
) (trip.Position, error) {
	// Валидация до транзакции: на заведомо плохой запрос не берём соединение
	// и не блокируем строку поездки.
	pos, err := trip.NewPosition(tripID, coords, recordedAt, u.now())
	if err != nil {
		return trip.Position{}, fmt.Errorf("add position to trip %s: %w", tripID, err)
	}

	var saved trip.Position
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.repo.GetByIDForUpdate(ctx, tripID)
		if err != nil {
			return err
		}
		if err := t.EnsureActive(); err != nil {
			return err
		}
		saved, err = u.repo.AddPosition(ctx, pos)
		if err != nil {
			return err
		}
		return u.repo.TouchLastPosition(ctx, tripID, pos.RecordedAt)
	})
	if err != nil {
		return trip.Position{}, fmt.Errorf("add position to trip %s: %w", tripID, err)
	}
	return saved, nil
}
