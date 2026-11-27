package trip

import (
	"time"

	"github.com/google/uuid"
)

// Position — точка маршрута поездки, присланная мобильным клиентом.
//
// RecordedAt — время фиксации на устройстве, а не время прихода на сервер:
// точки могут приходить пачкой и не по порядку (клиент был без сети).
type Position struct {
	ID          int64 // задаёт БД (BIGSERIAL), у новой точки 0
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
}

// NewPosition создаёт новую точку маршрута. Если координаты вне диапазона
// или время фиксации не задано, возвращается *ValidationError (HTTP 400).
func NewPosition(tripID uuid.UUID, coords Coordinates, recordedAt time.Time) (Position, error) {
	if err := coords.validate("coordinates"); err != nil {
		return Position{}, err
	}
	if recordedAt.IsZero() {
		return Position{}, &ValidationError{Field: "recorded_at", Reason: "is required"}
	}
	recordedAt = recordedAt.UTC().Truncate(time.Microsecond)

	return Position{ID: 0, TripID: tripID, Coordinates: coords, RecordedAt: recordedAt}, nil
}

// EnsureActive проверяет, что поездку ещё можно менять: завершать или
// добавлять в неё точки. Для завершённой поездки — ErrTripCompleted (HTTP 409).
func (t *Trip) EnsureActive() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}
