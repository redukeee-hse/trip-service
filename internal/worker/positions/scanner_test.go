package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

// fakeTicker — тикер под управлением теста. Канал небуферизованный:
// tick() возвращается, только когда сканер прочитал тик, — значит,
// предыдущий обход уже закончился и сканер снова ждёт в select.
type fakeTicker struct {
	c        chan time.Time
	interval time.Duration
	mu       sync.Mutex
	stopped  bool
}

func (f *fakeTicker) C() <-chan time.Time { return f.c }

func (f *fakeTicker) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
}

func (f *fakeTicker) isStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

func (f *fakeTicker) tick(t *testing.T) {
	t.Helper()
	select {
	case f.c <- time.Time{}:
	case <-time.After(2 * time.Second):
		t.Fatal("сканер не принял тик: завис в обходе или уже вышел")
	}
}

// fakeLister отдаёт заранее заданные ответы по очереди и запоминает вызовы.
type fakeLister struct {
	mu      sync.Mutex
	replies []listReply
	calls   []listCall
}

type listReply struct {
	trips []trip.StaleTrip
	err   error
}

type listCall struct {
	before time.Time
}

func (f *fakeLister) ListStale(_ context.Context, before time.Time) ([]trip.StaleTrip, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, listCall{before: before})
	if len(f.replies) == 0 {
		return nil, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r.trips, r.err
}

func (f *fakeLister) callList() []listCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]listCall(nil), f.calls...)
}

type fakeScanMetrics struct {
	mu     sync.Mutex
	counts map[positions.EnqueueResult]int
}

func (m *fakeScanMetrics) StaleCheck(_ context.Context, r string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counts == nil {
		m.counts = map[positions.EnqueueResult]int{}
	}
	m.counts[positions.EnqueueResult(r)]++
}

func (m *fakeScanMetrics) get(r positions.EnqueueResult) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[r]
}

var scanNow = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

type scannerEnv struct {
	ticker  *fakeTicker
	lister  *fakeLister
	queue   *positions.Queue
	metrics *fakeScanMetrics
	spans   *tracetest.SpanRecorder
	stop    func() // отменяет ctx и ждёт выхода Run
}

func startScanner(t *testing.T, queueSize int, replies ...listReply) *scannerEnv {
	t.Helper()
	env := &scannerEnv{
		ticker:  &fakeTicker{c: make(chan time.Time)},
		lister:  &fakeLister{replies: replies},
		queue:   positions.NewQueue(queueSize),
		metrics: &fakeScanMetrics{},
		spans:   tracetest.NewSpanRecorder(),
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(env.spans))
	s := positions.NewScanner(env.lister, env.queue,
		positions.ScannerConfig{Interval: 30 * time.Second, StaleAfter: time.Minute},
		func(d time.Duration) positions.Ticker {
			env.ticker.interval = d
			return env.ticker
		},
		func() time.Time { return scanNow },
		tp.Tracer("test"), env.metrics, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	var once sync.Once
	env.stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Run не вернулся после отмены ctx")
			}
		})
	}
	t.Cleanup(env.stop)
	return env
}

func staleTrips(n int) []trip.StaleTrip {
	out := make([]trip.StaleTrip, n)
	for i := range out {
		out[i] = trip.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	}
	return out
}

// Тик → выборка с границей now − STALE_AFTER по часам сервиса → поездки в очереди.
func TestScanner_QueuesStaleTrips(t *testing.T) {
	t.Parallel()

	trips := staleTrips(2)
	env := startScanner(t, 10, listReply{trips: trips})

	env.ticker.tick(t)
	env.stop() // Run вышел — обход точно закончен

	calls := env.lister.callList()
	if len(calls) != 1 {
		t.Fatalf("ListStale вызван %d раз, want 1 на один тик", len(calls))
	}
	if want := scanNow.Add(-time.Minute); !calls[0].before.Equal(want) {
		t.Errorf("ListStale(before=%v), want %v", calls[0].before, want)
	}
	if env.queue.Len() != 2 || env.metrics.get(positions.Queued) != 2 {
		t.Errorf("в очереди %d, queued=%d; want 2 и 2", env.queue.Len(), env.metrics.get(positions.Queued))
	}
	if env.ticker.interval != 30*time.Second {
		t.Errorf("тикер создан с периодом %v, want POSITION_CHECK_INTERVAL 30s", env.ticker.interval)
	}
	if !env.ticker.isStopped() {
		t.Error("тикер не остановлен после выхода Run")
	}
	if n := len(env.spans.Ended()); n != 1 || env.spans.Ended()[0].Name() != "positions.scan" {
		t.Errorf("спанов %d, want один positions.scan на обход", n)
	}
}

// До первого тика сканер в БД не ходит.
func TestScanner_NoTickNoScan(t *testing.T) {
	t.Parallel()

	env := startScanner(t, 10)
	env.stop()

	if n := len(env.lister.callList()); n != 0 {
		t.Errorf("ListStale вызван %d раз без тика", n)
	}
}

// Обязательный сценарий 2 на уровне сканера: следующий тик пришёл раньше,
// чем воркеры разобрали очередь, — повторно не ставим, считаем duplicate.
func TestScanner_DuplicateOnNextTick(t *testing.T) {
	t.Parallel()

	trips := staleTrips(2)
	env := startScanner(t, 10, listReply{trips: trips}, listReply{trips: trips})

	env.ticker.tick(t)
	env.ticker.tick(t)
	env.stop()

	if q, d := env.metrics.get(positions.Queued), env.metrics.get(positions.Duplicate); q != 2 || d != 2 {
		t.Errorf("queued=%d duplicate=%d, want 2 и 2", q, d)
	}
	if env.queue.Len() != 2 {
		t.Errorf("в очереди %d, want 2", env.queue.Len())
	}
}

// Обязательный сценарий 4 на уровне сканера: полная очередь — dropped,
// а сканер не блокируется и принимает следующий тик.
func TestScanner_DropsWhenFull(t *testing.T) {
	t.Parallel()

	env := startScanner(t, 1, listReply{trips: staleTrips(3)}, listReply{trips: staleTrips(1)})

	env.ticker.tick(t)
	env.ticker.tick(t) // принят — значит первый обход не завис на полной очереди
	env.stop()

	if q, d := env.metrics.get(positions.Queued), env.metrics.get(positions.Dropped); q != 1 || d != 3 {
		t.Errorf("queued=%d dropped=%d, want 1 и 3 (2 на первом тике, 1 на втором)", q, d)
	}
}

// Ошибка БД не останавливает сканер: на следующем тике он пробует снова.
func TestScanner_ListErrorDoesNotStop(t *testing.T) {
	t.Parallel()

	env := startScanner(t, 10,
		listReply{err: errors.New("db is down")},
		listReply{trips: staleTrips(1)})

	env.ticker.tick(t)
	env.ticker.tick(t)
	env.stop()

	if n := len(env.lister.callList()); n != 2 {
		t.Errorf("ListStale вызван %d раз, want 2: сканер должен пережить ошибку", n)
	}
	if env.metrics.get(positions.Queued) != 1 {
		t.Errorf("queued=%d, want 1 после восстановления БД", env.metrics.get(positions.Queued))
	}
}

// Сканер с noop-трейсером тоже работает (так он и запустится без телеметрии).
func TestScanner_NoopTracer(t *testing.T) {
	t.Parallel()

	tk := &fakeTicker{c: make(chan time.Time)}
	q := positions.NewQueue(10)
	s := positions.NewScanner(&fakeLister{replies: []listReply{{trips: staleTrips(1)}}}, q,
		positions.ScannerConfig{Interval: time.Second, StaleAfter: time.Second},
		func(time.Duration) positions.Ticker { return tk },
		time.Now, noop.NewTracerProvider().Tracer(""), &fakeScanMetrics{}, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	tk.tick(t)
	cancel()
	<-done

	if q.Len() != 1 {
		t.Errorf("в очереди %d, want 1", q.Len())
	}
}
