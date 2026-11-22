package get_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/usecase/trip/get"
)

func TestUsecase_Execute(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	stored := trip.Trip{
		ID:         id,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
	}
	errDB := errors.New("connection refused")

	tests := []struct {
		name     string
		repoTrip trip.Trip
		repoErr  error
		want     trip.Trip
		wantErr  error
	}{
		{
			name:     "найдена — возвращает поездку из репозитория",
			repoTrip: stored,
			want:     stored,
		},
		{
			name:    "не найдена — ErrTripNotFound сохраняется в цепочке",
			repoErr: trip.ErrTripNotFound,
			wantErr: trip.ErrTripNotFound,
		},
		{
			name:     "ошибка репозитория — оборачивается, данные не утекают",
			repoTrip: stored, // даже если репозиторий что-то вернул вместе с ошибкой
			repoErr:  errDB,
			wantErr:  errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			repo.EXPECT().
				GetByID(gomock.Any(), id).
				Return(tt.repoTrip, tt.repoErr).
				Times(1)

			got, err := get.New(repo).Execute(context.Background(), id)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want errors.Is(%v)", err, tt.wantErr)
				}
				if got != (trip.Trip{}) {
					t.Errorf("при ошибке ожидалась пустая поездка, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
