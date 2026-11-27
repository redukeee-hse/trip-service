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

// MaxRecordedAtSkew — насколько recorded_at может опережать часы сервера.
// Часы телефона и сервера расходятся, поэтому небольшой запас допустим;
// точка «из будущего» сверх него — ошибка клиента (ТЗ ЛР3, п. 4.1).
const MaxRecordedAtSkew = time.Minute

// NewPosition создаёт новую точку маршрута. now — текущее время сервиса:
// домен не зовёт time.Now() сам, чтобы правило проверялось тестом.
// Если координаты вне диапазона, время фиксации не задано или опережает
// now больше чем на MaxRecordedAtSkew, возвращается *ValidationError (HTTP 400).
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

// EnsureActive проверяет, что поездку ещё можно менять: завершать или
// добавлять в неё точки. Для завершённой поездки — ErrTripCompleted (HTTP 409).
func (t *Trip) EnsureActive() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}
