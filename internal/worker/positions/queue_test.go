package positions_test

import (
	"sync"
	"testing"
	"time"

	"github.com/redukeee-hse/avitoService/internal/worker/positions"
)

// enqueue вызывает Enqueue и падает, если тот заблокировался: сканер не
// имеет права ждать на записи в очередь (ЛР3 п. 5.3). time.After здесь —
// страховка от зависания теста, а не синхронизация.
func enqueue(t *testing.T, q *positions.Queue, tk positions.Task) positions.EnqueueResult {
	t.Helper()
	res := make(chan positions.EnqueueResult, 1)
	go func() { res <- q.Enqueue(tk) }()
	select {
	case r := <-res:
		return r
	case <-time.After(time.Second):
		t.Fatal("Enqueue заблокировался")
		return ""
	}
}

// Обязательный сценарий 2: поездка, уже стоящая в очереди, повторно не ставится.
func TestQueue_Duplicate(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	a := task()

	if got := enqueue(t, q, a); got != positions.Queued {
		t.Fatalf("первая постановка = %q, want queued", got)
	}
	if got := enqueue(t, q, a); got != positions.Duplicate {
		t.Errorf("повторная постановка = %q, want duplicate", got)
	}
	if got := enqueue(t, q, task()); got != positions.Queued {
		t.Errorf("другая поездка = %q, want queued", got)
	}
	if q.Len() != 2 {
		t.Errorf("Len = %d, want 2: дубликат не должен попасть в канал", q.Len())
	}
}

// Обязательный сценарий 4: переполнение даёт dropped, а не блокировку.
func TestQueue_OverflowDrops(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(2)
	enqueue(t, q, task())
	enqueue(t, q, task())

	dropped := task()
	if got := enqueue(t, q, dropped); got != positions.Dropped {
		t.Fatalf("третья в очередь на 2 = %q, want dropped", got)
	}
	// Дропнутая поездка не должна застрять в «сейчас в очереди»: иначе
	// на следующих тиках она вечно будет duplicate и её никогда не опросят.
	q2 := positions.NewQueue(1)
	enqueue(t, q2, dropped) // в свободную очередь та же поездка встаёт как обычно
	if got := enqueue(t, q, dropped); got != positions.Dropped {
		t.Errorf("повтор дропнутой в полную очередь = %q, want dropped, а не duplicate", got)
	}
}

// После Close постановка не паникует (send on closed channel), а отказывает.
func TestQueue_EnqueueAfterClose(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	q.Close()
	q.Close() // повторный Close безопасен

	if got := enqueue(t, q, task()); got != positions.Dropped {
		t.Errorf("после Close = %q, want dropped", got)
	}
}

// Отменённую поездку можно поставить снова: pending очищен.
func TestQueue_CancelFreesTrip(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(10)
	a := task()
	enqueue(t, q, a)
	q.Cancel(a.Trip.ID)

	if got := enqueue(t, q, a); got != positions.Queued {
		t.Errorf("после Cancel = %q, want queued", got)
	}
}

// Сканер, воркеры и хендлеры координат трогают очередь одновременно.
// Сам по себе тест почти ничего не проверяет — его смысл в `go test -race`:
// без mutex детектор гонок (или fatal error на map) уронит прогон.
func TestQueue_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	q := positions.NewQueue(1000)
	tasks := make([]positions.Task, 20)
	for i := range tasks {
		tasks[i] = task()
	}

	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 200 {
				tk := tasks[(g+i)%len(tasks)]
				if i%3 == 0 {
					q.Cancel(tk.Trip.ID)
				} else {
					q.Enqueue(tk)
				}
				_ = q.Len()
			}
		})
	}
	wg.Go(q.Close)
	wg.Wait()
}
