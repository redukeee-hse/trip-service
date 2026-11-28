package trip

import (
	"time"

	"github.com/google/uuid"
)

// StaleTrip — активная поездка, по которой давно нет координат.
type StaleTrip struct {
	ID         uuid.UUID
	DriverID   uuid.UUID
	LastSeenAt time.Time
}

// StaleCursor — ключ продолжения выборки; id нужен, потому что время у
// разных поездок совпадает.
type StaleCursor struct {
	LastSeenAt time.Time
	ID         uuid.UUID
}

func (s StaleTrip) Cursor() StaleCursor {
	return StaleCursor{LastSeenAt: s.LastSeenAt, ID: s.ID}
}
