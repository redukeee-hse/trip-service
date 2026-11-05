package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redukeee-hse/avitoService/internal/config"
)

func NewPool(ctx context.Context, db config.DBConfig) (*pgxpool.Pool, error) {

	poolConfig, err := pgxpool.ParseConfig(db.URL)
	if err != nil {
		return nil, fmt.Errorf("ошибка конфигурации PostgreSQL: %w", err)
	}

	poolConfig.MaxConns = db.MaxConns
	poolConfig.MinConns = db.MinConns
	poolConfig.MaxConnLifetime = db.MaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = db.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания пула: %w", err)
	}

	return pool, nil
}
