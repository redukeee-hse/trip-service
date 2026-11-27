package positions

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ScannerConfig struct {
	Interval   time.Duration
	StaleAfter time.Duration
	Limit      uint64 // поездок за тик; больше ёмкости очереди читать бессмысленно
}

// Scanner на каждом тике берёт из БД молчащие поездки и ставит их в очередь.
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

// Run блокируется до отмены ctx. Тики не накладываются: обход идёт в этой
// же горутине, а time.Ticker выбрасывает непрочитанные тики.
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

// scan — один обход. Ошибка БД сканер не останавливает.
func (s *Scanner) scan(ctx context.Context) {
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
	for _, tr := range trips {
		res := s.queue.Enqueue(Task{Trip: tr, Origin: span.SpanContext()})
		s.metrics.StaleCheck(ctx, string(res))
		if res == Dropped {
			s.log.WarnContext(ctx, "position request dropped: queue is full",
				slog.String("trip_id", tr.ID.String()))
		}
	}
}
