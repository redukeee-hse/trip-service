package transaction

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor — то, на чём репозиторий выполняет запрос: пул или открытая транзакция.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ Executor = (*pgxpool.Pool)(nil)
	_ Executor = pgx.Tx(nil)
)

type txKey struct{}

type Manager struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Manager {
	return &Manager{
		pool: pool,
	}
}

// Executor возвращает транзакцию из ctx, если вызов идёт внутри Do, иначе пул.
func (m *Manager) Executor(ctx context.Context) Executor {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return m.pool
}

// Do выполняет fn в транзакции. Вложенный Do переиспользует уже открытую
// транзакцию. Ошибка fn возвращается как есть, чтобы errors.Is по доменным
// ошибкам работал у вызывающего.
func (m *Manager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Rollback после успешного Commit вернёт pgx.ErrTxClosed — это ожидаемо.
	// WithoutCancel: откат должен дойти до базы, даже если ctx запроса уже отменён.
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(txCtx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
