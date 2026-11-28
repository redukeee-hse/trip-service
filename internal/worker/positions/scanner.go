package positions

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

type ScannerConfig struct {
	Interval   time.Duration
	StaleAfter time.Duration
	BatchSize  uint64 // строк в одной порции выборки
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

// scan читает порции, пока они не кончатся или очередь не откажет.
func (s *Scanner) scan(ctx context.Context) {
	ctx, span := s.tracer.Start(ctx, "positions.scan")
	defer span.End()

	before := s.now().Add(-s.cfg.StaleAfter)
	rowsRead := 0
	defer func() { s.metrics.StaleRowsRead(ctx, rowsRead) }()

	var after *trip.StaleCursor
	origin := span.SpanContext()

	for {
		batch, err := s.repo.ListStale(ctx, before, after, s.cfg.BatchSize)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "list stale trips")
			s.log.ErrorContext(ctx, "list stale trips failed", slog.Any("error", err))
			return
		}
		rowsRead += len(batch)

		for _, tr := range batch {
			res := s.queue.Enqueue(Task{Trip: tr, Origin: origin})
			s.metrics.StaleCheck(ctx, string(res))
			if res == Dropped {
				s.log.WarnContext(ctx, "position request dropped: queue is full",
					slog.String("trip_id", tr.ID.String()))
				return
			}
		}

		if len(batch) == 0 || uint64(len(batch)) < s.cfg.BatchSize {
			return
		}
		cursor := batch[len(batch)-1].Cursor()
		after = &cursor
	}
}
