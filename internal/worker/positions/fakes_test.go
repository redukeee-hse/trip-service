package positions_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

func task() positions.Task {
	return positions.Task{Trip: trip.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}}
}

// fakePusher — Push Service под контролем теста. fn решает, что ответить;
// calls — какие поездки опросили, по порядку.
type fakePusher struct {
	mu    sync.Mutex
	calls []uuid.UUID
	fn    func(ctx context.Context, tripID uuid.UUID) error
}

func (f *fakePusher) RequestPosition(ctx context.Context, tripID, _ uuid.UUID) error {
	f.mu.Lock()
	f.calls = append(f.calls, tripID)
	fn := f.fn
	f.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, tripID)
}

func (f *fakePusher) called() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.calls...)
}

// fakeMetrics считает результаты и сколько воркеров заняты одновременно.
type fakeMetrics struct {
	mu      sync.Mutex
	results map[positions.Result]int
	busy    int
	maxBusy int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{results: map[positions.Result]int{}}
}

func (m *fakeMetrics) WorkerBusy(context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy++
	m.maxBusy = max(m.maxBusy, m.busy)
}

func (m *fakeMetrics) WorkerIdle(context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy--
}

func (m *fakeMetrics) Request(_ context.Context, r string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[positions.Result(r)]++
}

func (m *fakeMetrics) count(r positions.Result) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[r]
}

func (m *fakeMetrics) max() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxBusy
}
