package transaction

import (
	"context"
	"fmt"
	"time"

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

// rollbackTimeout — сколько ждём ROLLBACK. Откат — одна короткая команда,
// поэтому несколько секунд хватает с запасом; дольше — значит БД недоступна.
const rollbackTimeout = 5 * time.Second

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
	defer func() {
		// WithoutCancel: откат должен дойти до базы, даже если клиент уже
		// оборвал запрос и ctx отменён. При этом значения ctx (спан трейса)
		// сохраняются, и ROLLBACK попадает в трейс запроса — context.Background
		// это потерял бы. WithTimeout поверх: WithoutCancel снимает и deadline,
		// и без своего срока откат при зависшей БД ждал бы вечно.
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
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
