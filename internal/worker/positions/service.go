package positions

import "context"

// Service запускает и останавливает сканер и пул вместе.
type Service struct {
	scanner *Scanner
	queue   *Queue
	pool    *Pool

	stopScanner context.CancelFunc
	scannerDone chan struct{}
	stopPool    context.CancelFunc // жёсткая остановка воркеров
	poolDone    chan struct{}
}

func NewService(scanner *Scanner, queue *Queue, pool *Pool) *Service {
	return &Service{scanner: scanner, queue: queue, pool: pool}
}

// Start запускает фон. Контексты не от сигнала: порядок остановки задаёт Stop.
func (s *Service) Start() {
	scanCtx, stopScanner := context.WithCancel(context.Background())
	poolCtx, stopPool := context.WithCancel(context.Background())
	s.stopScanner, s.stopPool = stopScanner, stopPool
	s.scannerDone, s.poolDone = make(chan struct{}), make(chan struct{})

	go func() {
		defer close(s.scannerDone)
		s.scanner.Run(scanCtx)
	}()
	go func() {
		defer close(s.poolDone)
		s.pool.Run(poolCtx)
	}()
}

// Stop останавливает сканер, затем даёт воркерам дообработать очередь до
// дедлайна ctx. Возвращает ctx.Err(), если их пришлось прервать.
func (s *Service) Stop(ctx context.Context) error {
	defer s.stopPool() // cancel вызываем всегда, и в успешной ветке тоже

	s.stopScanner()
	<-s.scannerDone

	s.queue.Close()

	select {
	case <-s.poolDone:
		return nil
	case <-ctx.Done():
		s.stopPool()
		<-s.poolDone
		return ctx.Err()
	}
}
