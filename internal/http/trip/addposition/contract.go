//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package addposition

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type positionAdder interface {
	Execute(ctx context.Context, tripID uuid.UUID, coords trip.Coordinates, recordedAt time.Time) (trip.Position, error)
}
