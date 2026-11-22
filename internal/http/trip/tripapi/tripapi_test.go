package tripapi_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	api "github.com/redukeee-hse/avitoService/internal/generated"
	"github.com/redukeee-hse/avitoService/internal/http/trip/tripapi"
)

func TestFromDomain(t *testing.T) {
	t.Parallel()

	finished := time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC)
	base := trip.Trip{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 59.94, Longitude: 30.31},
		Price:      1450,
		StartedAt:  time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name       string
		status     trip.Status
		finishedAt *time.Time
		wantStatus api.TripStatus
	}{
		{name: "активная — без finished_at", status: trip.StatusActive, wantStatus: api.TripStatus("active")},
		{name: "завершённая — с finished_at", status: trip.StatusCompleted, finishedAt: &finished, wantStatus: api.TripStatus("completed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := base
			in.Status, in.FinishedAt = tt.status, tt.finishedAt

			got := tripapi.FromDomain(in)

			want := api.Trip{
				Id:         in.ID,
				UserId:     in.UserID,
				DriverId:   in.DriverID,
				StartPoint: api.Coordinates{Latitude: 55.75, Longitude: 37.61},
				EndPoint:   api.Coordinates{Latitude: 59.94, Longitude: 30.31},
				Price:      1450,
				Status:     tt.wantStatus,
				StartedAt:  in.StartedAt,
				FinishedAt: tt.finishedAt,
			}
			if got != want {
				t.Errorf("got %+v\nwant %+v", got, want)
			}
		})
	}
}
