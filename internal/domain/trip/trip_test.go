package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

func TestTrip_Finish(t *testing.T) {
	t.Parallel()

	finishedAt := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	earlier := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		status         trip.Status
		finishedAt     *time.Time
		wantErr        error
		wantStatus     trip.Status
		wantFinishedAt *time.Time
	}{
		{
			name:           "active trip is finished",
			status:         trip.StatusActive,
			finishedAt:     nil,
			wantErr:        nil,
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: &finishedAt,
		},
		{
			name:           "completed trip is not changed",
			status:         trip.StatusCompleted,
			finishedAt:     &earlier,
			wantErr:        trip.ErrTripCompleted,
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: &earlier,
		},
		{
			name:           "unknown status is rejected",
			status:         trip.Status("cancelled"),
			finishedAt:     nil,
			wantErr:        trip.ErrTripCompleted,
			wantStatus:     trip.Status("cancelled"),
			wantFinishedAt: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := trip.Trip{Status: tt.status, FinishedAt: tt.finishedAt}

			err := tr.Finish(finishedAt)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tr.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", tr.Status, tt.wantStatus)
			}
			assertTimePtr(t, tr.FinishedAt, tt.wantFinishedAt)
		})
	}
}

func TestTrip_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modify    func(tr *trip.Trip)
		wantField string // пусто — ошибки быть не должно
	}{
		{name: "valid trip", modify: func(*trip.Trip) {}},
		{name: "boundary values are valid", modify: func(tr *trip.Trip) {
			tr.StartPoint = trip.Coordinates{Latitude: -90, Longitude: -180}
			tr.EndPoint = trip.Coordinates{Latitude: 90, Longitude: 180}
			tr.Price = 0
		}},
		{name: "empty user_id", modify: func(tr *trip.Trip) { tr.UserID = uuid.Nil }, wantField: "user_id"},
		{name: "empty driver_id", modify: func(tr *trip.Trip) { tr.DriverID = uuid.Nil }, wantField: "driver_id"},
		{name: "start latitude above 90", modify: func(tr *trip.Trip) { tr.StartPoint.Latitude = 90.1 }, wantField: "start_point.latitude"},
		{name: "start latitude below -90", modify: func(tr *trip.Trip) { tr.StartPoint.Latitude = -90.1 }, wantField: "start_point.latitude"},
		{name: "start longitude above 180", modify: func(tr *trip.Trip) { tr.StartPoint.Longitude = 180.1 }, wantField: "start_point.longitude"},
		{name: "end latitude below -90", modify: func(tr *trip.Trip) { tr.EndPoint.Latitude = -91 }, wantField: "end_point.latitude"},
		{name: "end longitude below -180", modify: func(tr *trip.Trip) { tr.EndPoint.Longitude = -180.1 }, wantField: "end_point.longitude"},
		{name: "negative price", modify: func(tr *trip.Trip) { tr.Price = -1 }, wantField: "price"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := validTrip()
			tt.modify(&tr)

			err := tr.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			var vErr *trip.ValidationError
			if !errors.As(err, &vErr) {
				t.Fatalf("err = %v, want *trip.ValidationError", err)
			}
			if vErr.Field != tt.wantField {
				t.Errorf("field = %q, want %q", vErr.Field, tt.wantField)
			}
			if vErr.Reason == "" {
				t.Error("reason must not be empty")
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	err := &trip.ValidationError{Field: "price", Reason: "must be >= 0"}

	if got, want := err.Error(), "price: must be >= 0"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func validTrip() trip.Trip {
	return trip.Trip{
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 59.93, Longitude: 30.33},
		Price:      1000,
		Status:     trip.StatusActive,
	}
}

func assertTimePtr(t *testing.T, got, want *time.Time) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("finished_at = %v, want %v", got, want)
	case !got.Equal(*want):
		t.Errorf("finished_at = %v, want %v", *got, *want)
	}
}
