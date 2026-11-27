package positions

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// pusher — то, что воркеру нужно от Push Service. Реализация —
// internal/gateway/push; интерфейс здесь, у потребителя (ЛР3 п. 5.5).
type pusher interface {
	RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
}

// Result — чем закончилась попытка запросить координату. Значения совпадают
// с лейблом result метрики tripgo.position_requests.
type Result string

const (
	ResultSuccess   Result = "success"
	ResultError     Result = "error"
	ResultCancelled Result = "cancelled" // координата пришла, пока задача ждала в очереди
)

// poolMetrics — метрики пула (docs/observability.md, ЛР3). Реализацию на
// OpenTelemetry добавим в infra/telemetry в задаче про метрики.
type poolMetrics interface {
	WorkerBusy(ctx context.Context)                                     // tripgo.position_requests.workers.active +1
	WorkerIdle(ctx context.Context)                                     // ... −1
	Request(ctx context.Context, result Result, duration time.Duration) // tripgo.position_requests и .duration
}
