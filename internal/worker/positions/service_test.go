package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

type serviceEnv struct {
	svc     *positions.Service
	queue   *positions.Queue
	ticker  *fakeTicker
	pusher  *fakePusher
	metrics *fakeMetrics
	scans   *fakeScanMetrics
}

func newService(t *testing.T, lister *blockingLister, pusher *fakePusher, queueSize int) *serviceEnv {
	t.Helper()
	env := &serviceEnv{
		queue:   positions.NewQueue(queueSize),
		ticker:  &fakeTicker{c: make(chan time.Time)},
		pusher:  pusher,
		metrics: newFakeMetrics(),
		scans:   &fakeScanMetrics{},
	}
	tracer := noop.NewTracerProvider().Tracer("")
	log := slog.New(slog.DiscardHandler)
	scanner := positions.NewScanner(lister, env.queue,
		positions.ScannerConfig{Interval: time.Second, StaleAfter: time.Second, Limit: 10},
		func(time.Duration) positions.Ticker { return env.ticker },
		time.Now, tracer, env.scans, log)
	pool := positions.NewPool(env.queue, pusher, 2, time.Second, env.metrics, tracer, log, time.Now)
	env.svc = positions.NewService(scanner, env.queue, pool)
	return env
}

// blockingLister — выборка, которую тест может задержать, чтобы Stop
// пришёл посреди обхода.
type blockingLister struct {
	entered chan struct{} // закрывается, когда обход вошёл в ListStale
	release chan struct{} // тест закрывает, чтобы ListStale вернулся
	trips   []trip.StaleTrip
}

func (l *blockingLister) ListStale(context.Context, time.Time, uint64) ([]trip.StaleTrip, error) {
	if l.entered != nil {
		close(l.entered)
		<-l.release
	}
	return l.trips, nil
}

// stop вызывает Stop в фоне и ждёт результат со страховкой от зависания.
func stop(t *testing.T, svc *positions.Service, ctx context.Context) error {
	t.Helper()
	res := make(chan error, 1)
	go func() { res <- svc.Stop(ctx) }()
	select {
	case err := <-res:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Stop не вернулся")
		return nil
	}
}

// Мягкая остановка: всё, что лежало в очереди, обработано; после Stop
// очередь закрыта, сканер не тикает, горутин нет (goleak в TestMain).
func TestService_StopDrainsQueue(t *testing.T) {
	t.Parallel()

	env := newService(t, &blockingLister{}, &fakePusher{}, 10)
	for range 5 {
		env.queue.Enqueue(task())
	}
	env.svc.Start()

	if err := stop(t, env.svc, context.Background()); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	if n := len(env.pusher.called()); n != 5 {
		t.Errorf("обработано %d задач, want 5: очередь должна быть дочитана", n)
	}
	if !env.ticker.isStopped() {
		t.Error("тикер сканера не остановлен")
	}
	if got := env.queue.Enqueue(task()); got != positions.Dropped {
		t.Errorf("Enqueue после Stop = %q, want dropped: очередь должна быть закрыта", got)
	}
}

// Шаг 2 до шага 3: Stop пришёл посреди обхода. Сканер обязан доделать
// обход и выйти ДО закрытия очереди — его поездка попадёт в очередь
// (queued, а не dropped) и будет обработана.
func TestService_StopWaitsForScanBeforeClosingQueue(t *testing.T) {
	t.Parallel()

	stale := trip.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	lister := &blockingLister{entered: make(chan struct{}), release: make(chan struct{}), trips: []trip.StaleTrip{stale}}
	env := newService(t, lister, &fakePusher{}, 10)
	env.svc.Start()

	env.ticker.tick(t)
	<-lister.entered // обход идёт, сканер внутри ListStale

	res := make(chan error, 1)
	go func() { res <- env.svc.Stop(context.Background()) }()

	select {
	case err := <-res:
		t.Fatalf("Stop вернулся (%v), пока сканер посреди обхода: его не дождались", err)
	default:
	}
	close(lister.release)

	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Stop = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop не вернулся")
	}
	if q, d := env.scans.get(positions.Queued), env.scans.get(positions.Dropped); q != 1 || d != 0 {
		t.Errorf("queued=%d dropped=%d, want 1 и 0: очередь закрыли раньше, чем сканер вышел", q, d)
	}
	if got := env.pusher.called(); len(got) != 1 || got[0] != stale.ID {
		t.Errorf("опрошены %v, want поездку из последнего обхода", got)
	}
}

// Жёсткая остановка: воркеры не успели в дедлайн — Stop прерывает их,
// дожидается выхода и возвращает ошибку дедлайна.
func TestService_StopDeadline(t *testing.T) {
	t.Parallel()

	inCall := make(chan struct{})
	pusher := &fakePusher{fn: func(ctx context.Context, _ uuid.UUID) error {
		select {
		case inCall <- struct{}{}:
		default:
		}
		<-ctx.Done() // зависший Push Service: вернёмся только по отмене
		return ctx.Err()
	}}
	env := newService(t, &blockingLister{}, pusher, 10)
	env.queue.Enqueue(task())
	env.svc.Start()
	<-inCall

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := stop(t, env.svc, ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
	}
	// Горутины воркеров к этому моменту обязаны завершиться — проверит goleak.
}
