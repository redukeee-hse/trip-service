package positions

import "time"

// Ticker — то, что сканеру нужно от тикера. Спрятан за интерфейс, чтобы
// тесты не ждали реальные 30 секунд: в тесте канал C отдаёт тик тогда,
// когда тест сам в него запишет (ЛР3 п. 7, «время подменяемо»).
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// NewTimeTicker — настоящий тикер на time.Ticker для прода.
func NewTimeTicker(d time.Duration) Ticker {
	return timeTicker{t: time.NewTicker(d)}
}

type timeTicker struct {
	t *time.Ticker
}

func (t timeTicker) C() <-chan time.Time { return t.t.C }
func (t timeTicker) Stop()               { t.t.Stop() }
