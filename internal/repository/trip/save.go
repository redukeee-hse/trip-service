package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

func (r *Repository) Create(ctx context.Context, t trip.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Insert("trips").
		SetMap(map[string]any{
			"id":              t.ID,
			"user_id":         t.UserID,
			"driver_id":       t.DriverID,
			"start_latitude":  t.StartPoint.Latitude,
			"start_longitude": t.StartPoint.Longitude,
			"end_latitude":    t.EndPoint.Latitude,
			"end_longitude":   t.EndPoint.Longitude,
			"price":           t.Price,
			colStatus:         t.Status,
			"started_at":      t.StartedAt,
		}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	if _, err := r.db.Executor(ctx).Exec(ctx, query, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "trips_driver_active_uniq" {
			return trip.ErrDriverBusy
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

func (r *Repository) AddStatusHistory(ctx context.Context, tripID uuid.UUID, from *trip.Status, to trip.Status) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status").
		Values(tripID, from, to).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status history query: %w", err)
	}

	if _, err := r.db.Executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}
	return nil
}

func (r *Repository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Update("trips").
		SetMap(map[string]any{
			colStatus:     trip.StatusCompleted,
			"finished_at": finishedAt,
			"updated_at":  finishedAt,
		}).
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build finish trip query: %w", err)
	}

	if _, err := r.db.Executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("finish trip: %w", err)
	}
	return nil
}
