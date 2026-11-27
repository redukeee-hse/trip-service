// Package listpositions — маршрут поездки: все её точки по порядку.
package listpositions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type Usecase struct {
	repo tripRepository
}

func New(repo tripRepository) *Usecase {
	return &Usecase{repo: repo}
}

// Execute возвращает точки маршрута; для несуществующей поездки — ErrTripNotFound.
func (u *Usecase) Execute(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	_, err := u.repo.GetByID(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("list positions of trip %s: %w", tripID, err)
	}
	tp, err := u.repo.ListPositions(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("list positions of trip %s: %w", tripID, err)
	}
	return tp, nil
}
