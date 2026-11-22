package trip

import (
	"context"

	"github.com/redukeee-hse/avitoService/internal/infra/postgres/transaction"
)

type executorProvider interface {
	Executor(ctx context.Context) transaction.Executor
}
