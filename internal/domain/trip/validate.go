package trip

import "github.com/google/uuid"

const (
	minLatitude  = -90.0
	maxLatitude  = 90.0
	minLongitude = -180.0
	maxLongitude = 180.0
)

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
