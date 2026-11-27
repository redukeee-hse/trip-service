package trip

import (
	"time"

	"github.com/google/uuid"
)

// Position — точка маршрута. RecordedAt — время фиксации на устройстве.
type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
}

// MaxRecordedAtSkew — насколько recorded_at может опережать часы сервера.
const MaxRecordedAtSkew = time.Minute

// NewPosition возвращает *ValidationError, если координаты вне диапазона
// или recorded_at не задан либо опережает now больше чем на MaxRecordedAtSkew.
func NewPosition(tripID uuid.UUID, coords Coordinates, recordedAt, now time.Time) (Position, error) {
	if err := coords.validate("coordinates"); err != nil {
		return Position{}, err
	}
	if recordedAt.IsZero() {
		return Position{}, &ValidationError{Field: "recorded_at", Reason: "is required"}
	}

	if recordedAt.Sub(now) > MaxRecordedAtSkew {
		return Position{}, &ValidationError{Field: "recorded_at", Reason: "must not be in the future"}
	}
	recordedAt = recordedAt.UTC().Truncate(time.Microsecond)

	return Position{ID: 0, TripID: tripID, Coordinates: coords, RecordedAt: recordedAt}, nil
}

// EnsureActive возвращает ErrTripCompleted для завершённой поездки.
func (t *Trip) EnsureActive() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}
