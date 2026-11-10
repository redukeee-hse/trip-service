//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
)

type tripCreator interface {
	Execute(ctx context.Context, t trip.Trip, idempotencyKey *api.IdempotencyKey) (trip.Trip, bool, error)
}
