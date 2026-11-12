package get

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

func (u *Usecase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	t, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip %s: %w", id, err)
	}
	return t, nil
}
