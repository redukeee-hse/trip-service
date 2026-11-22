package trip

import (
	"time"

	sq "github.com/Masterminds/squirrel"
)

type Repository struct {
	db           executorProvider
	queryTimeout time.Duration
	sb           sq.StatementBuilderType
}

func New(db executorProvider, queryTimeout time.Duration) *Repository {
	return &Repository{
		db:           db,
		queryTimeout: queryTimeout,
		sb:           sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}
