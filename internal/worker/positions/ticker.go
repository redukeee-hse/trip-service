package positions

import "time"

// Ticker спрятан за интерфейс, чтобы тесты управляли тиками сами.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

func NewTimeTicker(d time.Duration) Ticker {
	return timeTicker{t: time.NewTicker(d)}
}

type timeTicker struct {
	t *time.Ticker
}

func (t timeTicker) C() <-chan time.Time { return t.t.C }
func (t timeTicker) Stop()               { t.t.Stop() }
