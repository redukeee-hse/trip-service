package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// lastSeenExpr должно совпадать с выражением индекса trips_active_stale_idx,
// иначе индекс не применится.
const lastSeenExpr = "COALESCE(last_position_at, started_at)"

// ListStale возвращает активные поездки, молчащие с before, самые давние
// первыми. before считается по часам сервиса, чтобы его можно было подменить в тестах.
func (r *Repository) ListStale(ctx context.Context, before time.Time, limit uint64) ([]trip.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Select("id", "driver_id", lastSeenExpr).
		From("trips").
		// Литерал, а не $1: иначе generic-план не применит частичный индекс.
		Where(colStatus+" = '"+string(trip.StatusActive)+"'").
		Where(sq.Lt{lastSeenExpr: before}).
		OrderBy(lastSeenExpr, "id").
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select stale trips query: %w", err)
	}

	rows, err := r.db.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale trips: %w", err)
	}
	defer rows.Close()

	stale := make([]trip.StaleTrip, 0)
	for rows.Next() {
		var s trip.StaleTrip
		if err := rows.Scan(&s.ID, &s.DriverID, &s.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		s.LastSeenAt = s.LastSeenAt.UTC()
		stale = append(stale, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return stale, nil
}
