//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type positionLister interface {
	Execute(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error)
}
