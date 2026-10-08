package business

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redukeee-hse/avitoService/internal/database"
	"github.com/redukeee-hse/avitoService/internal/model"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type TripService struct {
	tx   TxManager
	repo *database.TripRepository
}

func NewTripService(tx TxManager, repo *database.TripRepository) *TripService {
	return &TripService{tx: tx, repo: repo}
}

func (s *TripService) CreateTrip(ctx context.Context, trip model.Trip) (model.Trip, error) {
	trip.ID = uuid.New()
	trip.Status = model.StatusActive
	trip.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	trip.FinishedAt = nil

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, trip); err != nil {
			return err
		}
		return s.repo.AddStatusHistory(ctx, trip.ID, nil, model.StatusActive)
	})
	if err != nil {
		return model.Trip{}, err
	}
	return trip, nil
}

func (s *TripService) GetTrip(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *TripService) FinishTrip(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	var result model.Trip

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		trip, err := s.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if trip.Status == model.StatusCompleted {
			return model.ErrTripCompleted
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if err := s.repo.Finish(ctx, id, now); err != nil {
			return err
		}
		from := model.StatusActive
		if err := s.repo.AddStatusHistory(ctx, id, &from, model.StatusCompleted); err != nil {
			return err
		}
		trip.Status = model.StatusCompleted
		trip.FinishedAt = &now
		result = trip
		return nil
	})
	if err != nil {
		return model.Trip{}, err
	}
	return result, nil
}
