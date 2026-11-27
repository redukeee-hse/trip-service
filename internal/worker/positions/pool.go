package positions

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/redukeee-hse/avitoService/internal/gateway/push"
)

// Pool — фиксированное число воркеров, которые живут от старта до остановки
// сервиса и берут задачи из одной очереди. Горутину на задачу не создаём:
// при 10 000 молчащих поездок это 10 000 одновременных запросов в Push
// Service; пул ограничивает параллелизм числом воркеров.
type Pool struct {
	queue   *Queue
	push    pusher
	workers int
	timeout time.Duration // на одну задачу (PUSH_REQUEST_TIMEOUT)
	metrics poolMetrics
	log     *slog.Logger
	now     func() time.Time
}

func NewPool(
	queue *Queue,
	push pusher,
	workers int,
	timeout time.Duration,
	metrics poolMetrics,
	log *slog.Logger,
	now func() time.Time,
) *Pool {
	return &Pool{
		queue:   queue,
		push:    push,
		workers: workers,
		timeout: timeout,
		metrics: metrics,
		log:     log,
		now:     now,
	}
}

// Run запускает воркеров и блокируется, пока все они не завершатся.
//
// Воркер выходит в двух случаях:
//   - очередь закрыта (Queue.Close) и опустела — штатная остановка: всё,
//     что успели поставить, обработано;
//   - отменён ctx — жёсткая остановка по SHUTDOWN_TIMEOUT: не успели
//     дообработать, бросаем.
//
// Run возвращается только когда вышли ВСЕ воркеры — ни одной горутины пула
// после него не остаётся (это проверяет goleak).
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

// worker — цикл одного воркера: взять задачу, обработать, вернуться за
// следующей. Ошибка одной задачи цикл не прерывает.
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

// handle обрабатывает одну задачу.
func (p *Pool) handle(ctx context.Context, t Task) {
	id := t.Trip.ID

	// Координата пришла, пока задача лежала в канале: Cancel убрал поездку
	// из pending. Опрашивать водителя уже незачем.
	if !p.queue.isPending(id) {
		p.metrics.Request(ctx, ResultCancelled, 0)
		return
	}
	// Освобождаем поездку после обработки, а не до: пока идёт вызов, новый
	// тик сканера не должен поставить её второй раз.
	defer p.queue.done(id)

	p.metrics.WorkerBusy(ctx)
	defer p.metrics.WorkerIdle(ctx)

	// Таймаут на задачу. http.Client гейтвея и так ограничен тем же
	// значением, но ctx — общий механизм: им же прерывается вызов при
	// жёсткой остановке, и он уйдёт в спан.
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	start := p.now()
	err := p.push.RequestPosition(callCtx, id, t.Trip.DriverID)
	elapsed := p.now().Sub(start)

	if err != nil {
		p.metrics.Request(ctx, ResultError, elapsed)
		level := slog.LevelError
		if errors.Is(err, push.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			// Недоступность соседа — Warn по docs/observability.md.
			level = slog.LevelWarn
		}
		p.log.Log(ctx, level, "request position failed",
			slog.String("trip_id", id.String()),
			slog.String("driver_id", t.Trip.DriverID.String()),
			slog.Any("error", err))
		return
	}
	p.metrics.Request(ctx, ResultSuccess, elapsed)
}
