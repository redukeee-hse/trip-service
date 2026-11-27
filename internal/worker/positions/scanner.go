package positions

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ScannerConfig — параметры сканера.
type ScannerConfig struct {
	Interval   time.Duration // POSITION_CHECK_INTERVAL — период тикера
	StaleAfter time.Duration // POSITION_STALE_AFTER — сколько молчания считается «давно»
	// Limit — сколько поездок читать за тик. Больше, чем влезает в очередь,
	// читать бессмысленно — лишнее всё равно уйдёт в dropped. В container
	// передаём POSITION_QUEUE_SIZE. (Чтение порциями — задание со звёздочкой.)
	Limit uint64
}

// Scanner — одна фоновая горутина: на каждом тике берёт из БД молчащие
// поездки и ставит их в очередь (ЛР3 п. 5.3). Состояние поездок в памяти не
// держит — каждый тик спрашивает базу заново, поэтому после рестарта опрос
// продолжается сам.
type Scanner struct {
	repo      staleLister
	queue     *Queue
	cfg       ScannerConfig
	newTicker func(time.Duration) Ticker
	now       func() time.Time
	tracer    trace.Tracer
	metrics   scannerMetrics
	log       *slog.Logger
}

func NewScanner(
	repo staleLister,
	queue *Queue,
	cfg ScannerConfig,
	newTicker func(time.Duration) Ticker,
	now func() time.Time,
	tracer trace.Tracer,
	metrics scannerMetrics,
	log *slog.Logger,
) *Scanner {
	return &Scanner{
		repo:      repo,
		queue:     queue,
		cfg:       cfg,
		newTicker: newTicker,
		now:       now,
		tracer:    tracer,
		metrics:   metrics,
		log:       log,
	}
}

// Run крутит тикер до отмены ctx. Блокируется; запускать в своей горутине.
//
// Тики не накладываются по построению: обход (s.scan) выполняется в этой
// же горутине, и пока он идёт, следующий тик никто не читает. Отдельную
// горутину на обход не запускаем — иначе медленная БД дала бы несколько
// обходов одновременно. А time.Ticker сам выбрасывает тики, которые
// никто не прочитал: в его канале место на один тик, и после долгого
// обхода придёт один тик, а не пачка пропущенных.
func (s *Scanner) Run(ctx context.Context) {
	s.log.InfoContext(ctx, "position scanner started",
		slog.Duration("interval", s.cfg.Interval),
		slog.Duration("stale_after", s.cfg.StaleAfter))
	defer s.log.InfoContext(context.WithoutCancel(ctx), "position scanner stopped")

	ticker := s.newTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			s.scan(ctx)
		}
	}
}

// scan — один обход: выборка из БД и постановка в очередь.
// Ошибка БД сканер не останавливает: залогировали, ждём следующего тика.
func (s *Scanner) scan(ctx context.Context) {
	// Спан на каждый обход. Его SpanContext кладём в задачу — воркер свяжет
	// с ним свой спан через span link.
	ctx, span := s.tracer.Start(ctx, "positions.scan")
	defer span.End()

	before := s.now().Add(-s.cfg.StaleAfter)
	trips, err := s.repo.ListStale(ctx, before, s.cfg.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list stale trips")
		s.log.ErrorContext(ctx, "list stale trips failed", slog.Any("error", err))
		return
	}
	// Enqueue не блокируется: полная очередь не тормозит обход, дропнутые
	// поездки попадут в выборку на следующем тике.
	for _, tr := range trips {
		res := s.queue.Enqueue(Task{Trip: tr, Origin: span.SpanContext()})
		s.metrics.StaleCheck(ctx, res)
		if res == Dropped {
			s.log.WarnContext(ctx, "position request dropped: queue is full",
				slog.String("trip_id", tr.ID.String()))
		}
	}
}
