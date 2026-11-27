// Package positions — опрос устаревших поездок: сканер, очередь и пул воркеров.
package positions

import (
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// Task — задача воркеру: опросить водителя поездки.
type Task struct {
	Trip   trip.StaleTrip
	Origin trace.SpanContext // спан обхода сканера, для span link
}

// EnqueueResult — значения лейбла result метрики tripgo.stale_checks.
type EnqueueResult string

const (
	Queued    EnqueueResult = "queued"
	Duplicate EnqueueResult = "duplicate" // поездка уже в очереди или в обработке
	Dropped   EnqueueResult = "dropped"   // очередь полна или закрыта
)

// Queue — ограниченная очередь задач с дедупликацией по поездке. pending —
// поездки в очереди или в обработке; его меняют сканер, воркеры и ручка
// координат, поэтому доступ только под mu.
type Queue struct {
	tasks chan Task

	mu      sync.Mutex
	pending map[uuid.UUID]struct{}
	closed  bool
}

func NewQueue(size int) *Queue {
	return &Queue{
		tasks:   make(chan Task, size),
		pending: make(map[uuid.UUID]struct{}, size),
	}
}

// Enqueue не блокируется: при полной очереди сразу возвращает Dropped,
// чтобы медленный Push Service не остановил сканер.
func (q *Queue) Enqueue(t Task) EnqueueResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return Dropped
	}
	_, ok := q.pending[t.Trip.ID]
	if ok {
		return Duplicate
	}

	select {
	case q.tasks <- t:
		q.pending[t.Trip.ID] = struct{}{}
		return Queued
	default:
		return Dropped
	}
}

// Cancel отменяет опрос поездки: пришла координата. Задача остаётся в
// канале, воркер её пропустит.
func (q *Queue) Cancel(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, tripID)
}

// Close запрещает новые задачи; воркеры дочитают канал и выйдут.
// Вызывать после остановки сканера.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.tasks)
}

// Len — сколько задач в канале, для gauge queue.size.
func (q *Queue) Len() int {
	return len(q.tasks)
}

func (q *Queue) isPending(tripID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.pending[tripID]
	return ok
}

func (q *Queue) done(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, tripID)
}
