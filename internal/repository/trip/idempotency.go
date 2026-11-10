package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

func (r *Repository) ClaimIdempotencyKey(ctx context.Context, key uuid.UUID, requestData string, tripID uuid.UUID, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	deleteQuery, deleteArgs, err := r.sb.
		Delete("idempotency_keys").
		Where(sq.Eq{"key": key}).
		Where(sq.Lt{"created_at": time.Now().Add(-ttl)}).
		ToSql()
	if err != nil {
		return false, fmt.Errorf("build delete expired idempotency key query: %w", err)
	}
	if _, err := r.db.Executor(ctx).Exec(ctx, deleteQuery, deleteArgs...); err != nil {
		return false, fmt.Errorf("delete expired idempotency key: %w", err)
	}

	insertQuery, insertArgs, err := r.sb.
		Insert("idempotency_keys").
		Columns("key", "request_data", "trip_id").
		Values(key, requestData, tripID).
		Suffix("ON CONFLICT (key) DO NOTHING").
		ToSql()
	if err != nil {
		return false, fmt.Errorf("build insert idempotency key query: %w", err)
	}
	tag, err := r.db.Executor(ctx).Exec(ctx, insertQuery, insertArgs...)
	if err != nil {
		return false, fmt.Errorf("insert idempotency key: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) GetIdempotencyKey(ctx context.Context, key uuid.UUID) (string, uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Select("request_data", "trip_id").
		From("idempotency_keys").
		Where(sq.Eq{"key": key}).
		ToSql()
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("build select idempotency key query: %w", err)
	}

	var (
		requestData string
		tripID      uuid.UUID
	)
	if err := r.db.Executor(ctx).QueryRow(ctx, query, args...).Scan(&requestData, &tripID); err != nil {
		return "", uuid.Nil, fmt.Errorf("select idempotency key: %w", err)
	}
	return requestData, tripID, nil
}
