package trip

import "errors"

var (
	ErrTripNotFound  = errors.New("поездка не найдена")
	ErrDriverBusy    = errors.New("у водителя уже есть активная поездка")
	ErrTripCompleted = errors.New("поездка уже завершена")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Reason
}
