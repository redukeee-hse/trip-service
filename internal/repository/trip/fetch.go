package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.getByID(ctx, id, false)
}

// GetByIDForUpdate берёт строку под блокировку FOR UPDATE; осмысленно только внутри транзакции.
func (r *Repository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.getByID(ctx, id, true)
}

func (r *Repository) getByID(ctx context.Context, id uuid.UUID, forUpdate bool) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	builder := r.sb.
		Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id})
	if forUpdate {
		builder = builder.Suffix("FOR UPDATE")
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build select trip query: %w", err)
	}

	t, err := scanTrip(r.db.Executor(ctx).QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Trip{}, trip.ErrTripNotFound
		}
		return trip.Trip{}, fmt.Errorf("select trip: %w", err)
	}
	return t, nil
}

// CountActive — сколько поездок сейчас в статусе active. Источник для gauge
// tripgo.trips.active: значение берётся из БД, а не считается в памяти
// сервиса, поэтому оно верно после рестарта и при нескольких копиях сервиса.
// Запрос дешёвый: по активным поездкам есть частичный индекс
// trips_driver_active_uniq.
func (r *Repository) CountActive(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Select("count(*)").
		From("trips").
		Where(sq.Eq{colStatus: trip.StatusActive}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count active trips query: %w", err)
	}

	var n int64
	if err := r.db.Executor(ctx).QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active trips: %w", err)
	}
	return n, nil
}
