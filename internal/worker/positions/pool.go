package positions

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/gateway/push"
)

// Pool — фиксированное число воркеров на всё время работы сервиса.
type Pool struct {
	queue   *Queue
	push    pusher
	workers int
	timeout time.Duration // на одну задачу
	metrics poolMetrics
	tracer  trace.Tracer
	log     *slog.Logger
	now     func() time.Time
}

func NewPool(
	queue *Queue,
	push pusher,
	workers int,
	timeout time.Duration,
	metrics poolMetrics,
	tracer trace.Tracer,
	log *slog.Logger,
	now func() time.Time,
) *Pool {
	return &Pool{
		queue:   queue,
		push:    push,
		workers: workers,
		timeout: timeout,
		metrics: metrics,
		tracer:  tracer,
		log:     log,
		now:     now,
	}
}

// Run блокируется, пока не выйдут все воркеры: очередь закрыта и пуста
// (мягкая остановка) или отменён ctx (жёсткая).
func (p *Pool) Run(ctx context.Context) {
	p.log.InfoContext(ctx, "position workers started", slog.Int("workers", p.workers))
	defer p.log.InfoContext(context.WithoutCancel(ctx), "position workers stopped")

	var wg sync.WaitGroup
	for range p.workers {
		wg.Go(func() {
			p.worker(ctx)
		})
	}

	wg.Wait()
}

func (p *Pool) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-p.queue.tasks:
			if !ok {
				return
			}
			p.handle(ctx, t)
		}
	}
}

func (p *Pool) handle(ctx context.Context, t Task) {
	id := t.Trip.ID

	// Link, а не родитель: обход сканера заканчивается раньше обработки.
	ctx, span := p.tracer.Start(ctx, "positions.request",
		trace.WithNewRoot(),
		trace.WithLinks(trace.Link{SpanContext: t.Origin}),
		trace.WithAttributes(attribute.String("trip.id", id.String())))
	defer span.End()

	// Координата пришла, пока задача ждала в очереди.
	if !p.queue.isPending(id) {
		p.metrics.Request(ctx, string(ResultCancelled), 0)
		span.SetAttributes(attribute.String("result", string(ResultCancelled)))
		return
	}
	// После вызова, а не до: пока идёт вызов, сканер не поставит поездку снова.
	defer p.queue.done(id)

	p.metrics.WorkerBusy(ctx)
	defer p.metrics.WorkerIdle(ctx)

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	start := p.now()
	err := p.push.RequestPosition(callCtx, id, t.Trip.DriverID)
	elapsed := p.now().Sub(start)

	if err != nil {
		p.metrics.Request(ctx, string(ResultError), elapsed)
		span.RecordError(err)
		span.SetStatus(codes.Error, "request position")
		span.SetAttributes(attribute.String("result", string(ResultError)))
		level := slog.LevelError
		if errors.Is(err, push.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			level = slog.LevelWarn
		}
		p.log.Log(ctx, level, "request position failed",
			slog.String("trip_id", id.String()),
			slog.String("driver_id", t.Trip.DriverID.String()),
			slog.Any("error", err))
		return
	}
	p.metrics.Request(ctx, string(ResultSuccess), elapsed)
	span.SetAttributes(attribute.String("result", string(ResultSuccess)))
}
