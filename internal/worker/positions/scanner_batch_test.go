package positions_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

// pagedLister — выборка, которая честно режет отсортированный набор на
// порции по ключу (время, id), как keyset-запрос в БД.
type pagedLister struct {
	mu    sync.Mutex
	all   []trip.StaleTrip // отсортированы по (LastSeenAt, ID)
	calls []listCall
}

func newPagedLister(n int, sameTime bool) *pagedLister {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	all := make([]trip.StaleTrip, n)
	for i := range all {
		at := base.Add(time.Duration(i) * time.Second)
		if sameTime {
			at = base
		}
		all[i] = trip.StaleTrip{ID: uuid.New(), DriverID: uuid.New(), LastSeenAt: at}
	}
	slices.SortFunc(all, cmpStale)
	return &pagedLister{all: all}
}

func cmpStale(a, b trip.StaleTrip) int {
	if c := a.LastSeenAt.Compare(b.LastSeenAt); c != 0 {
		return c
	}
	return bytes.Compare(a.ID[:], b.ID[:]) // так Postgres сравнивает uuid
}

func (l *pagedLister) ListStale(_ context.Context, before time.Time, after *trip.StaleCursor, limit uint64) ([]trip.StaleTrip, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, listCall{before: before, after: after, limit: limit})
	out := []trip.StaleTrip{}
	for _, s := range l.all {
		if after != nil && cmpStale(s, trip.StaleTrip{LastSeenAt: after.LastSeenAt, ID: after.ID}) <= 0 {
			continue
		}
		out = append(out, s)
		if uint64(len(out)) == limit {
			break
		}
	}
	return out, nil
}

func (l *pagedLister) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// runOneTick запускает сканер, даёт один тик и останавливает: Run вернулся —
// обход закончен.
func runOneTick(t *testing.T, lister *pagedLister, q *positions.Queue, batch uint64) *fakeScanMetrics {
	t.Helper()
	tk := &fakeTicker{c: make(chan time.Time)}
	m := &fakeScanMetrics{}
	s := positions.NewScanner(lister, q,
		positions.ScannerConfig{Interval: time.Second, StaleAfter: time.Second, BatchSize: batch},
		func(time.Duration) positions.Ticker { return tk },
		time.Now, noop.NewTracerProvider().Tracer(""), m, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	tk.tick(t)
	cancel()
	<-done
	return m
}

// ТЗ п. 11: 250 поездок, порция 100, очередь вмещает все — три запроса,
// все 250 в очереди.
func TestScanner_Batches_AllFit(t *testing.T) {
	t.Parallel()

	lister := newPagedLister(250, false)
	q := positions.NewQueue(300)
	m := runOneTick(t, lister, q, 100)

	if n := lister.callCount(); n != 3 {
		t.Errorf("запросов %d, want 3 (100 + 100 + 50)", n)
	}
	if q.Len() != 250 || m.get(positions.Queued) != 250 {
		t.Errorf("в очереди %d, queued=%d, want 250", q.Len(), m.get(positions.Queued))
	}
	if r := m.rows(); len(r) != 1 || r[0] != 250 {
		t.Errorf("rows_read = %v, want [250] — одно значение за тик", r)
	}
	if lister.calls[0].after != nil || lister.calls[0].limit != 100 {
		t.Errorf("первая порция: after=%v limit=%d, want без курсора и limit 100", lister.calls[0].after, lister.calls[0].limit)
	}
	// Продолжение — с последней строки предыдущей порции.
	if c := lister.calls[1].after; c == nil || c.ID != lister.all[99].ID {
		t.Errorf("вторая порция после %v, want после 100-й строки %s", c, lister.all[99].ID)
	}
}

// ТЗ п. 11: 250 поездок, порция 100, очередь на 50 — после первой порции
// сканер остановился, второго запроса не было.
func TestScanner_Batches_StopOnFirstDrop(t *testing.T) {
	t.Parallel()

	lister := newPagedLister(250, false)
	q := positions.NewQueue(50)
	m := runOneTick(t, lister, q, 100)

	if n := lister.callCount(); n != 1 {
		t.Errorf("запросов %d, want 1: первый dropped — сигнал остановиться", n)
	}
	if m.get(positions.Queued) != 50 || m.get(positions.Dropped) != 1 {
		t.Errorf("queued=%d dropped=%d, want 50 и 1", m.get(positions.Queued), m.get(positions.Dropped))
	}
	if r := m.rows(); len(r) != 1 || r[0] != 100 {
		t.Errorf("rows_read = %v, want [100]", r)
	}
}

// ТЗ п. 11: у поездок одинаковое время и они на границе порций — ни одна не
// потерялась и не прочиталась дважды. Одного времени в курсоре здесь мало.
func TestScanner_Batches_SameTimeOnBoundary(t *testing.T) {
	t.Parallel()

	lister := newPagedLister(25, true) // у всех одно время
	q := positions.NewQueue(100)
	m := runOneTick(t, lister, q, 10)

	if m.get(positions.Queued) != 25 || m.get(positions.Duplicate) != 0 {
		t.Errorf("queued=%d duplicate=%d, want 25 и 0: потеряли или перечитали поездки",
			m.get(positions.Queued), m.get(positions.Duplicate))
	}
	if n := lister.callCount(); n != 3 {
		t.Errorf("запросов %d, want 3 (10 + 10 + 5)", n)
	}
}

// Ровно полная последняя порция: следующий запрос вернёт пустоту, и обход
// на ней закончится, а не зациклится.
func TestScanner_Batches_ExactMultiple(t *testing.T) {
	t.Parallel()

	lister := newPagedLister(200, false)
	q := positions.NewQueue(300)
	m := runOneTick(t, lister, q, 100)

	if m.get(positions.Queued) != 200 {
		t.Errorf("queued=%d, want 200", m.get(positions.Queued))
	}
	if n := lister.callCount(); n != 3 {
		t.Errorf("запросов %d, want 3 (100 + 100 + пустая)", n)
	}
}

// Следующий тик начинает снова с первой порции.
func TestScanner_Batches_NextTickFromStart(t *testing.T) {
	t.Parallel()

	lister := newPagedLister(30, false)
	tk := &fakeTicker{c: make(chan time.Time)}
	q := positions.NewQueue(10)
	s := positions.NewScanner(lister, q,
		positions.ScannerConfig{Interval: time.Second, StaleAfter: time.Second, BatchSize: 20},
		func(time.Duration) positions.Ticker { return tk },
		time.Now, noop.NewTracerProvider().Tracer(""), &fakeScanMetrics{}, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	tk.tick(t)
	tk.tick(t)
	cancel()
	<-done

	lister.mu.Lock()
	defer lister.mu.Unlock()
	if len(lister.calls) != 2 || lister.calls[1].after != nil {
		t.Errorf("вызовы %+v, want 2, второй тик — с первой порции (after == nil)", lister.calls)
	}
}
