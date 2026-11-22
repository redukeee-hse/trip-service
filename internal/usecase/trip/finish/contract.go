//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type tripRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	AddStatusHistory(ctx context.Context, tripID uuid.UUID, from *trip.Status, to trip.Status) error
	Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// tripMetrics — бизнес-счётчик завершённых поездок (tripgo.trips.completed).
type tripMetrics interface {
	TripCompleted(ctx context.Context)
}
