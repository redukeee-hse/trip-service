package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

func newPool(q *positions.Queue, p *fakePusher, workers int, m *fakeMetrics) *positions.Pool {
	return positions.NewPool(q, p, workers, time.Second, m, noop.NewTracerProvider().Tracer(""), slog.New(slog.DiscardHandler), time.Now)
}

// startPool запускает Run в фоне; wait ждёт его возврата (со страховкой
// от зависания).
func startPool(t *testing.T, ctx context.Context, pool *positions.Pool) (wait func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Run(ctx)
	}()
	return func() {
		t.Helper()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Run не вернулся: воркеры не завершились")
		}
	}
}

// Обязательный сценарий 5: N задач обрабатывают ровно POSITION_WORKERS
// воркеров — параллельно, но не больше.
func TestPool_ProcessesWithFixedWorkers(t *testing.T) {
	t.Parallel()

	const workers, n = 3, 10
	started := make(chan struct{}, n)
	release := make(chan struct{})
	p := &fakePusher{fn: func(context.Context, uuid.UUID) error {
		started <- struct{}{}
		<-release // держим воркер занятым, пока тест не отпустит
		return nil
	}}
	q := positions.NewQueue(n)
	m := newFakeMetrics()
	for range n {
		q.Enqueue(task())
	}

	wait := startPool(t, context.Background(), newPool(q, p, workers, m))

	// Все три воркера взяли по задаче одновременно — значит их правда три.
	for range workers {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("заняты меньше воркеров, чем POSITION_WORKERS")
		}
	}
	close(release)
	q.Close() // штатная остановка: воркеры дорабатывают очередь и выходят
	wait()

	if got := len(p.called()); got != n {
		t.Errorf("обработано %d задач, want %d: при Close очередь должна быть дочитана", got, n)
	}
	if got := m.max(); got != workers {
		t.Errorf("одновременно занято %d воркеров, want ровно %d", got, workers)
	}
	if got := m.count(positions.ResultSuccess); got != n {
		t.Errorf("success = %d, want %d", got, n)
	}
}

// Обязательный сценарий 3: координата пришла между постановкой и
// обработкой — в Push Service не идём.
func TestPool_CancelledTaskSkipped(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	cancelled, kept := task(), task()
	q.Enqueue(cancelled)
	q.Enqueue(kept)
	q.Cancel(cancelled.Trip.ID) // хендлер координат
	q.Close()

	p := &fakePusher{}
	m := newFakeMetrics()
	startPool(t, context.Background(), newPool(q, p, 1, m))()

	if got := p.called(); !slices.Equal(got, []uuid.UUID{kept.Trip.ID}) {
		t.Errorf("опрошены %v, want только %s", got, kept.Trip.ID)
	}
	if m.count(positions.ResultCancelled) != 1 || m.count(positions.ResultSuccess) != 1 {
		t.Errorf("метрики %v, want cancelled=1 success=1", m.results)
	}
}

// Обязательный сценарий 7: ошибка Push Service не роняет воркер.
func TestPool_ErrorDoesNotStopWorker(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	failing, next := task(), task()
	q.Enqueue(failing)
	q.Enqueue(next)
	q.Close()

	p := &fakePusher{fn: func(_ context.Context, id uuid.UUID) error {
		if id == failing.Trip.ID {
			return errors.New("push service is down")
		}
		return nil
	}}
	m := newFakeMetrics()
	startPool(t, context.Background(), newPool(q, p, 1, m))() // один воркер — следующая задача на нём же

	if got := p.called(); !slices.Equal(got, []uuid.UUID{failing.Trip.ID, next.Trip.ID}) {
		t.Errorf("опрошены %v, want обе задачи по порядку", got)
	}
	if m.count(positions.ResultError) != 1 || m.count(positions.ResultSuccess) != 1 {
		t.Errorf("метрики %v, want error=1 success=1", m.results)
	}
}

// Пока идёт вызов, поездка остаётся «в очереди»: следующий тик её второй
// раз не поставит. Освобождается — после обработки, а не до.
func TestPool_TripBusyWhileProcessing(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	a := task()
	inCall := make(chan struct{})
	release := make(chan struct{})
	p := &fakePusher{fn: func(context.Context, uuid.UUID) error {
		close(inCall)
		<-release
		return nil
	}}
	q.Enqueue(a)
	wait := startPool(t, context.Background(), newPool(q, p, 1, newFakeMetrics()))

	<-inCall
	if got := q.Enqueue(a); got != positions.Duplicate {
		t.Errorf("постановка во время вызова = %q, want duplicate", got)
	}
	close(release)
	q.Close()
	wait()
}

// Каждая задача ограничена таймаутом: зависший сосед не держит воркер вечно.
func TestPool_TaskTimeout(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	q.Enqueue(task())
	q.Close()

	p := &fakePusher{fn: func(ctx context.Context, _ uuid.UUID) error {
		<-ctx.Done() // висим, пока не сработает таймаут задачи
		return ctx.Err()
	}}
	m := newFakeMetrics()
	pool := positions.NewPool(q, p, 1, 20*time.Millisecond, m, noop.NewTracerProvider().Tracer(""), slog.New(slog.DiscardHandler), time.Now)
	startPool(t, context.Background(), pool)()

	if m.count(positions.ResultError) != 1 {
		t.Errorf("метрики %v, want error=1 по таймауту", m.results)
	}
}

// Обязательный сценарий 6: по отмене контекста воркеры завершаются, даже
// если очередь не закрыта и вызов в Push Service висит. Утечки горутин
// ловит goleak в TestMain.
func TestPool_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	q.Enqueue(task())
	inCall := make(chan struct{})
	p := &fakePusher{fn: func(ctx context.Context, _ uuid.UUID) error {
		close(inCall)
		<-ctx.Done() // вызов прервётся только отменой
		return ctx.Err()
	}}

	ctx, cancel := context.WithCancel(context.Background())
	wait := startPool(t, ctx, newPool(q, p, 2, newFakeMetrics()))

	<-inCall
	cancel()
	wait() // и занятый, и простаивающий воркер вышли
}

// Спан воркера связан со спаном обхода через span link, а не через родителя:
// обработка асинхронна и переживает обход (docs/observability.md, ЛР3).
func TestPool_SpanLinkToScan(t *testing.T) {
	t.Parallel()

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	_, scan := tp.Tracer("test").Start(context.Background(), "positions.scan")
	scan.End() // обход закончился раньше, чем воркер взял задачу

	q := positions.NewQueue(1)
	tk := task()
	tk.Origin = scan.SpanContext()
	q.Enqueue(tk)
	q.Close()

	pool := positions.NewPool(q, &fakePusher{}, 1, time.Second, newFakeMetrics(), tp.Tracer("test"),
		slog.New(slog.DiscardHandler), time.Now)
	startPool(t, context.Background(), pool)()

	var req sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "positions.request" {
			req = s
		}
	}
	if req == nil {
		t.Fatal("нет спана positions.request")
	}
	if req.Parent().IsValid() {
		t.Error("спан воркера дочерний — нужен link, а не родитель")
	}
	if req.SpanContext().TraceID() == scan.SpanContext().TraceID() {
		t.Error("спан воркера в трейсе обхода — должен быть своим корнем")
	}
	links := req.Links()
	if len(links) != 1 || links[0].SpanContext.SpanID() != scan.SpanContext().SpanID() {
		t.Errorf("links = %+v, want одна ссылка на спан обхода %s", links, scan.SpanContext().SpanID())
	}
}
