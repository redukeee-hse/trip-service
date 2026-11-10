package finish

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
	now     func() time.Time
	metrics tripMetrics
	log     *slog.Logger
}

func New(tx txManager, repo tripRepository, now func() time.Time, metrics tripMetrics, log *slog.Logger) *Usecase {
	return &Usecase{repo: repo, tx: tx, now: now, metrics: metrics, log: log}
}

func (u *Usecase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	var result trip.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		from := t.Status
		now := u.now().UTC().Truncate(time.Microsecond)
		if err := t.Finish(now); err != nil {
			return err
		}
		err = u.repo.Finish(ctx, id, now)
		if err != nil {
			return err
		}
		if err := u.repo.AddStatusHistory(ctx, id, &from, t.Status); err != nil {
			return err
		}
		result = t
		return nil
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip %s: %w", id, err)
	}
	// Счётчик — только после успешного коммита: откатившаяся транзакция
	// поездку не завершила.
	u.metrics.TripCompleted(ctx)
	u.log.InfoContext(ctx, "trip completed", slog.String("trip_id", result.ID.String()))
	return result, nil
}
