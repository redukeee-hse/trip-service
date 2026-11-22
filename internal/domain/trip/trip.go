package trip

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

const (
	minLatitude  = -90.0
	maxLatitude  = 90.0
	minLongitude = -180.0
	maxLongitude = 180.0
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Status string

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
}

// New создаёт новую поездку. Новая поездка всегда активна и не завершена:
// статус и FinishedAt задаёт сам конструктор, снаружи их передать нельзя,
// поэтому неправильную новую поездку создать невозможно. Если данные
// нарушают правила модели, возвращается *ValidationError.
//
// id и startedAt передаются аргументами, а не генерируются внутри: домен
// не зависит от генератора id и часов, и его можно проверить тестом.
//
// Поездку, прочитанную из БД, конструктор не создаёт: это не новая поездка,
// а восстановление сохранённой, она может быть и завершённой.
func New(
	id, userID, driverID uuid.UUID,
	startPoint, endPoint Coordinates,
	price int64,
	startedAt time.Time,
) (Trip, error) {
	t := Trip{
		ID:         id,
		UserID:     userID,
		DriverID:   driverID,
		StartPoint: startPoint,
		EndPoint:   endPoint,
		Price:      price,
		Status:     StatusActive,
		StartedAt:  startedAt,
	}
	if err := t.Validate(); err != nil {
		return Trip{}, err
	}
	return t, nil
}

func (t *Trip) Finish(finishedAt time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	t.Status = StatusCompleted
	t.FinishedAt = &finishedAt
	return nil
}

// Validate проверяет правила модели поездки. Возвращает *ValidationError
// с именем поля и причиной, чтобы транспорт мог показать клиенту детали.
func (t *Trip) Validate() error {
	if t.UserID == uuid.Nil {
		return &ValidationError{Field: "user_id", Reason: "must not be empty"}
	}
	if t.DriverID == uuid.Nil {
		return &ValidationError{Field: "driver_id", Reason: "must not be empty"}
	}
	if err := t.StartPoint.validate("start_point"); err != nil {
		return err
	}
	if err := t.EndPoint.validate("end_point"); err != nil {
		return err
	}
	if t.Price < 0 {
		return &ValidationError{Field: "price", Reason: "must be >= 0"}
	}
	return nil
}

// validate проверяет диапазоны координат. field — префикс для имени поля
// в ошибке, чтобы было видно, какая из двух точек неверна.
func (c Coordinates) validate(field string) error {
	if c.Latitude < minLatitude || c.Latitude > maxLatitude {
		return &ValidationError{Field: field + ".latitude", Reason: "must be in [-90, 90]"}
	}
	if c.Longitude < minLongitude || c.Longitude > maxLongitude {
		return &ValidationError{Field: field + ".longitude", Reason: "must be in [-180, 180]"}
	}
	return nil
}
