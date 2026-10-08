package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redukeee-hse/avitoService/internal/model"
)

type TripRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	sb           sq.StatementBuilderType
}

func NewTripRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *TripRepository {
	return &TripRepository{
		pool:         pool,
		queryTimeout: queryTimeout,
		sb:           sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

func (r *TripRepository) Create(ctx context.Context, trip model.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Insert("trips").
		SetMap(map[string]any{
			"id":              trip.ID,
			"user_id":         trip.UserID,
			"driver_id":       trip.DriverID,
			"start_latitude":  trip.StartPoint.Latitude,
			"start_longitude": trip.StartPoint.Longitude,
			"end_latitude":    trip.EndPoint.Latitude,
			"end_longitude":   trip.EndPoint.Longitude,
			"price":           trip.Price,
			"status":          trip.Status,
			"started_at":      trip.StartedAt,
		}).
		ToSql()

	if err != nil {
		return fmt.Errorf("запрос создания поездки: %w", err)
	}

	if _, err := executorFrom(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "trips_driver_active_uniq" {
			return model.ErrDriverBusy
		}
		return fmt.Errorf("создание поездки: %w", err)
	}

	return nil
}

func (r *TripRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	return r.getByID(ctx, id, false)
}

func (r *TripRepository) AddStatusHistory(ctx context.Context, tripID uuid.UUID, from *model.TripStatus, to model.TripStatus) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status").
		Values(tripID, from, to).
		ToSql()
	if err != nil {
		return fmt.Errorf("запрос истории статусов: %w", err)
	}

	if _, err := executorFrom(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("вставка истории статусов: %w", err)
	}
	return nil
}

func (r *TripRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	return r.getByID(ctx, id, true)
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Update("trips").
		SetMap(map[string]any{
			"status":      model.StatusCompleted,
			"finished_at": finishedAt,
			"updated_at":  finishedAt,
		}).
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("запрос завершения поездки: %w", err)
	}

	if _, err := executorFrom(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("завершение поездки: %w", err)
	}
	return nil
}

func (r *TripRepository) getByID(ctx context.Context, id uuid.UUID, forUpdate bool) (model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	builder := r.sb.
		Select("user_id", "driver_id", "start_latitude", "start_longitude", "end_latitude", "end_longitude", "price", "status", "started_at", "finished_at").
		From("trips").
		Where(sq.Eq{"id": id})
	if forUpdate {
		builder = builder.Suffix("FOR UPDATE")
	}

	query, args, err := builder.ToSql()

	if err != nil {
		return model.Trip{}, fmt.Errorf("запрос получения поездки: %w", err)
	}

	trip := model.Trip{
		ID: id,
	}
	err = executorFrom(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&trip.UserID,
		&trip.DriverID,
		&trip.StartPoint.Latitude,
		&trip.StartPoint.Longitude,
		&trip.EndPoint.Latitude,
		&trip.EndPoint.Longitude,
		&trip.Price,
		&trip.Status,
		&trip.StartedAt,
		&trip.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Trip{}, model.ErrTripNotFound
		}
		return model.Trip{}, fmt.Errorf("получение поездки: %w", err)
	}
	return trip, nil
}
