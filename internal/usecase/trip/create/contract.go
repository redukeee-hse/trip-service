//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type tripRepository interface {
	AddStatusHistory(ctx context.Context, tripID uuid.UUID, from *trip.Status, to trip.Status) error
	Create(ctx context.Context, trip trip.Trip) error
	GetByID(ctx context.Context, tripID uuid.UUID) (trip.Trip, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type idempotencyRepository interface {
	ClaimIdempotencyKey(ctx context.Context, key uuid.UUID, requestData string, tripID uuid.UUID, ttl time.Duration) (bool, error)
	GetIdempotencyKey(ctx context.Context, key uuid.UUID) (requestData string, tripID uuid.UUID, err error)
}

// tripMetrics — бизнес-счётчик созданных поездок (tripgo.trips.created).
// Реализация в infra/telemetry; usecase про OpenTelemetry не знает.
type tripMetrics interface {
	TripCreated(ctx context.Context)
}
