package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/redukeee-hse/avitoService/internal/domain/trip"
)

// lastSeenExpr — «когда по поездке последний раз было что-то известно»:
// время последней координаты, а если их не было — started_at.
//
// Выражение ОБЯЗАНО совпадать с индексом trips_active_stale_idx буква в
// букву (миграция 20261127130000), иначе Postgres индекс не применит и на
// каждом тике сканера будет seq scan по всей таблице.
const lastSeenExpr = "COALESCE(last_position_at, started_at)"

// ListStale возвращает активные поездки, по которым ничего не было известно
// раньше границы before, — самые давние первыми, не больше limit штук.
//
// before считает вызывающий: now - POSITION_STALE_AFTER по часам сервиса.
// Так сравнение идёт в SQL, а «сейчас» подменяется в тестах (now() базы
// подменить нельзя).
func (r *Repository) ListStale(ctx context.Context, before time.Time, limit uint64) ([]trip.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := r.sb.
		Select("id", "driver_id", lastSeenExpr).
		From("trips").
		// Статус литералом, а не параметром: индекс частичный (WHERE status =
		// 'active'), и планировщик применит его, только если видит в запросе
		// то же значение. С `status = $1` generic-план (pgx кеширует prepared
		// statements) не может это доказать и уходит в seq scan — проверено
		// EXPLAIN. Значение — наша константа, не ввод клиента, инъекции нет.
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
