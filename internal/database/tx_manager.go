package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ executor = (*pgxpool.Pool)(nil)
	_ executor = (pgx.Tx)(nil)
)

type txKey struct{}

type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{
		pool: pool,
	}
}

func executorFrom(ctx context.Context, pool *pgxpool.Pool) executor {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

func (txManager *TxManager) Do(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		if err := fn(ctx); err != nil {
			return fmt.Errorf("начало работы внутренней функции: %w", err)
		}
		return nil
	}
	tx, err := txManager.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)

	defer tx.Rollback(txCtx)
	if err := fn(txCtx); err != nil {
		return fmt.Errorf("ошибка работы функции: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return fmt.Errorf("коммит транзакции: %w", err)
	}

	return nil
}
