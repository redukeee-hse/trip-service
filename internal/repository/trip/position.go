package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// AddPosition сохраняет точку и возвращает её с id из БД.
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

// TouchLastPosition сдвигает last_position_at только вперёд: сравнение в
// самом UPDATE, опоздавшая точка строку не меняет.
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

// ListPositions возвращает маршрут поездки: все точки по возрастанию
// recorded_at, при равенстве — по id. Пустой маршрут — пустой срез, не ошибка.
// Существование поездки здесь не проверяется — это делает use case.
func (r *Repository) ListPositions(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select positions query: %w", err)
	}

	rows, err := r.db.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select positions: %w", err)
	}
	defer rows.Close()

	positions := make([]trip.Position, 0)
	for rows.Next() {
		var p trip.Position
		if err := rows.Scan(&p.ID, &p.TripID, &p.Coordinates.Latitude, &p.Coordinates.Longitude, &p.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		p.RecordedAt = p.RecordedAt.UTC()
		positions = append(positions, p)
	}
	// Ошибка могла случиться посреди чтения (обрыв соединения, таймаут):
	// rows.Next() вернёт false, и без этой проверки мы отдали бы обрезанный маршрут.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return positions, nil
}
