// Package positions — опрос устаревших поездок: сканер находит молчащие
// поездки, кладёт задачи в очередь, пул воркеров просит Push Service
// прислать координату (ЛР3 п. 5.3–5.4). Сканер, очередь и воркеры — один
// компонент, поэтому один пакет.
package positions

import (
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// Task — задача воркеру: опросить водителя поездки.
type Task struct {
	Trip trip.StaleTrip
	// Origin — спан проверки сканера, породившей задачу. Воркер свяжет
	// с ним свой спан через span link (не через родителя: обработка идёт
	// асинхронно и переживает тик сканера). Заполнит сканер — задача 6.
	Origin trace.SpanContext
}

// EnqueueResult — что очередь сделала с задачей. Значения совпадают с
// лейблом result метрики tripgo.stale_checks.
type EnqueueResult string

const (
	Queued    EnqueueResult = "queued"    // поставили в очередь
	Duplicate EnqueueResult = "duplicate" // поездка уже в очереди или в обработке
	Dropped   EnqueueResult = "dropped"   // очередь полна (или закрыта) — не ждём, отказываем
)

// Queue — ограниченная очередь задач с дедупликацией по поездке.
//
// Канал сам по себе не умеет ответить «эта поездка уже внутри?», поэтому
// рядом лежит pending — множество поездок, которые сейчас в очереди или в
// обработке. В него одновременно пишут три стороны:
//   - сканер (Enqueue) — добавляет;
//   - воркер (done) — убирает после обработки;
//   - хендлер координат (Cancel) — убирает, если координата пришла раньше.
//
// Поэтому любой доступ к pending и closed — только под mu. Гонка на map в Go —
// не «неверное значение», а fatal error, процесс падает целиком.
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

// Enqueue ставит задачу в очередь и НИКОГДА не блокируется: если очередь
// полна, сразу возвращает Dropped. Иначе один медленный Push Service
// остановил бы сканер, а с ним проверку всех поездок. Дропнутая поездка не
// теряется — она всё ещё молчит и попадёт в выборку на следующем тике.
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

// Cancel убирает поездку из «сейчас в очереди»: по ней пришла координата,
// опрашивать больше не нужно. Саму задачу из канала достать нельзя — она
// останется там, и воркер, взяв её, увидит отмену (isPending == false) и в
// Push Service не пойдёт. Вызывает обработчик POST /positions.
func (q *Queue) Cancel(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, tripID)
}

// Close запрещает новые задачи и закрывает канал: воркеры дочитают то, что
// уже лежит в очереди, и выйдут. Вызывать после остановки сканера
// (ЛР3 п. 5.7, шаги 2–3). Повторный вызов безопасен.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.tasks)
}

// Len — сколько задач лежит в канале (для gauge queue.size). Отменённые,
// но ещё не вынутые воркером задачи тоже считаются: место они занимают.
func (q *Queue) Len() int {
	return len(q.tasks)
}

// isPending — поездка всё ещё ждёт опроса (не отменена координатой).
func (q *Queue) isPending(tripID uuid.UUID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.pending[tripID]
	return ok
}

// done — воркер закончил с поездкой: со следующего тика её снова можно ставить.
func (q *Queue) done(tripID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, tripID)
}
