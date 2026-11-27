package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type pusher interface {
	RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
}

// Result — значения лейбла result метрики tripgo.position_requests.
type Result string

const (
	ResultSuccess   Result = "success"
	ResultError     Result = "error"
	ResultCancelled Result = "cancelled" // координата пришла, пока задача ждала в очереди
)

type poolMetrics interface {
	WorkerBusy(ctx context.Context)
	WorkerIdle(ctx context.Context)
	Request(ctx context.Context, result Result, duration time.Duration)
}

type staleLister interface {
	ListStale(ctx context.Context, before time.Time, limit uint64) ([]trip.StaleTrip, error)
}

type scannerMetrics interface {
	StaleCheck(ctx context.Context, result EnqueueResult)
}
