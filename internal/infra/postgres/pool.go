package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/redukeee-hse/avitoService/internal/config"
)

// New создаёт пул соединений. Каждый запрос пула инструментирован:
// спан с db.operation.name и db.collection.name и метрика
// db.client.operation.duration (см. queryTracer).
func New(ctx context.Context, db config.DBConfig, tp trace.TracerProvider, mp metric.MeterProvider) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(db.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	poolConfig.MaxConns = db.MaxConns
	poolConfig.MinConns = db.MinConns
	poolConfig.MaxConnLifetime = db.MaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = db.ConnectTimeout

	tracer, err := newQueryTracer(tp, mp)
	if err != nil {
		return nil, fmt.Errorf("query tracer: %w", err)
	}
	poolConfig.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}
