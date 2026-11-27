//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package addposition

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type tripRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	AddPosition(ctx context.Context, p trip.Position) (trip.Position, error)
	TouchLastPosition(ctx context.Context, tripID uuid.UUID, at time.Time) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type positionRequests interface {
	Cancel(tripID uuid.UUID)
}

type positionMetrics interface {
	PositionSaved(ctx context.Context)
}
