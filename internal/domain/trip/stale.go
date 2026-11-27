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
