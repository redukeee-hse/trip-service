package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

func TestNewPosition(t *testing.T) {
	t.Parallel()

	tripID := uuid.New()
	coords := trip.Coordinates{Latitude: 59.9343, Longitude: 30.3351}
	// Не UTC и с наносекундами: конструктор должен привести к UTC и обрезать
	// до микросекунд — ровно столько хранит TIMESTAMPTZ.
	recordedAt := time.Date(2026, 11, 27, 13, 5, 0, 123456789, time.FixedZone("MSK", 3*60*60))
	want := time.Date(2026, 11, 27, 10, 5, 0, 123456000, time.UTC)

	p, err := trip.NewPosition(tripID, coords, recordedAt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.ID != 0 || p.TripID != tripID || p.Coordinates != coords {
		t.Errorf("position = %+v, want new point of trip %s at %+v", p, tripID, coords)
	}
	if !p.RecordedAt.Equal(want) || p.RecordedAt.Location() != time.UTC {
		t.Errorf("RecordedAt = %v, want %v (UTC, микросекунды)", p.RecordedAt, want)
	}
}

func TestNewPosition_Invalid(t *testing.T) {
	t.Parallel()

	ok := trip.Coordinates{Latitude: 0, Longitude: 0}
	at := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		coords    trip.Coordinates
		at        time.Time
		wantField string
	}{
		{"широта > 90", trip.Coordinates{Latitude: 90.0001, Longitude: 0}, at, "coordinates.latitude"},
		{"широта < -90", trip.Coordinates{Latitude: -90.0001, Longitude: 0}, at, "coordinates.latitude"},
		{"долгота > 180", trip.Coordinates{Latitude: 0, Longitude: 180.0001}, at, "coordinates.longitude"},
		{"долгота < -180", trip.Coordinates{Latitude: 0, Longitude: -180.0001}, at, "coordinates.longitude"},
		{"нет recorded_at", ok, time.Time{}, "recorded_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := trip.NewPosition(uuid.New(), tt.coords, tt.at)
			var vErr *trip.ValidationError
			if !errors.As(err, &vErr) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if vErr.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", vErr.Field, tt.wantField)
			}
		})
	}
}

// Границы диапазона включительны: полюс и линия перемены дат — валидные точки.
func TestNewPosition_Bounds(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	for _, c := range []trip.Coordinates{
		{Latitude: 90, Longitude: 180},
		{Latitude: -90, Longitude: -180},
	} {
		if _, err := trip.NewPosition(uuid.New(), c, at); err != nil {
			t.Errorf("NewPosition(%+v) err = %v, want nil", c, err)
		}
	}
}

func TestTrip_EnsureActive(t *testing.T) {
	t.Parallel()

	active := trip.Trip{Status: trip.StatusActive}
	if err := active.EnsureActive(); err != nil {
		t.Errorf("active: err = %v, want nil", err)
	}
	completed := trip.Trip{Status: trip.StatusCompleted}
	if err := completed.EnsureActive(); !errors.Is(err, trip.ErrTripCompleted) {
		t.Errorf("completed: err = %v, want ErrTripCompleted", err)
	}
}
