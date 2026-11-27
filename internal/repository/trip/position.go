package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// AddPosition сохраняет точку маршрута и возвращает её с id из БД.
// Вызывается внутри транзакции use case, после блокировки строки поездки.
func (r *Repository) AddPosition(ctx context.Context, p trip.Position) (trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(p.TripID, p.Coordinates.Latitude, p.Coordinates.Longitude, p.RecordedAt).
		Suffix("RETURNING id").
		ToSql()
	if err != nil {
		return trip.Position{}, fmt.Errorf("build insert position query: %w", err)
	}

	if err := r.db.Executor(ctx).QueryRow(ctx, query, args...).Scan(&p.ID); err != nil {
		return trip.Position{}, fmt.Errorf("insert position: %w", err)
	}
	return p, nil
}

// TouchLastPosition сдвигает trips.last_position_at вперёд до at.
//
// Значение только растёт: опоздавшая точка (at раньше уже записанного)
// строку не меняет. Сравнение — в самом SQL, а не в Go: так проверка и
// запись атомарны. Ноль обновлённых строк — нормальный исход, не ошибка.
func (r *Repository) TouchLastPosition(ctx context.Context, tripID uuid.UUID, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Update("trips").
		Set("last_position_at", at).
		Set("updated_at", at).
		Where(sq.Eq{"id": tripID}).
		Where(sq.Or{
			sq.Eq{"last_position_at": nil},
			sq.Lt{"last_position_at": at},
		}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build touch last position query: %w", err)
	}

	if _, err := r.db.Executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("touch last position: %w", err)
	}
	return nil
}
