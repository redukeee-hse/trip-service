package trip

import (
	"time"

	"github.com/google/uuid"
)

// StaleTrip — активная поездка, по которой координаты давно не приходили:
// её водителю сканер отправит команду прислать координату.
type StaleTrip struct {
	ID       uuid.UUID
	DriverID uuid.UUID // получатель push-команды
	// LastSeenAt — время последней координаты, а если их не было —
	// started_at. Нужен для сортировки и для логов: «молчит с ...».
	LastSeenAt time.Time
}
