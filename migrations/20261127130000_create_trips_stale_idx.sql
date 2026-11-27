-- +goose NO TRANSACTION
-- CONCURRENTLY не блокирует запись в trips и не работает внутри транзакции.

-- +goose Up
-- Под выборку сканера: частичный по активным, по выражению из ORDER BY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS trips_active_stale_idx
    ON trips ((COALESCE(last_position_at, started_at)), id)
    WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS trips_active_stale_idx;
