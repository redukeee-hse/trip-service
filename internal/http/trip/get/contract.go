//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package get

import (
	"context"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// tripGetter — то, что ручке нужно от usecase get. Объявлен здесь, у
// потребителя; реализует его *usecase/trip/get.Usecase.
type tripGetter interface {
	Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error)
}
