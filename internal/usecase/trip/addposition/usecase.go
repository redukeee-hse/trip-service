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
	tx       txManager
	repo     tripRepository
	requests positionRequests
	metrics  positionMetrics
	now      func() time.Time
}

func New(tx txManager, repo tripRepository, requests positionRequests, metrics positionMetrics, now func() time.Time) *Usecase {
	return &Usecase{tx: tx, repo: repo, requests: requests, metrics: metrics, now: now}
}

// Execute сохраняет точку и сдвигает last_position_at в одной транзакции.
func (u *Usecase) Execute(
	ctx context.Context,
	tripID uuid.UUID,
	coords trip.Coordinates,
	recordedAt time.Time,
) (trip.Position, error) {
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

	// После коммита: откатившаяся транзакция точку не сохранила.
	u.requests.Cancel(tripID)
	u.metrics.PositionSaved(ctx)
	return saved, nil
}
