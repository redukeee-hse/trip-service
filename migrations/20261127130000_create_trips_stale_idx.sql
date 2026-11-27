-- +goose NO TRANSACTION
-- CREATE INDEX CONCURRENTLY нельзя выполнять внутри транзакции, а goose
-- по умолчанию оборачивает миграцию в неё. CONCURRENTLY нужен, потому что
-- trips — живая таблица: обычный CREATE INDEX запретил бы INSERT/UPDATE
-- поездок на всё время построения.

-- +goose Up
-- Индекс под выборку поездок для опроса (сканер, ЛР3 п. 5.2):
--   WHERE status = 'active' AND COALESCE(last_position_at, started_at) < $1
--   ORDER BY COALESCE(last_position_at, started_at), id
--
-- Частичный (WHERE status = 'active'): завершённые поездки копятся вечно,
-- а в индекс попадают только активные — он маленький, и завершённая
-- поездка сама из него выпадает.
-- По выражению, а не по колонке: запрос фильтрует и сортирует по COALESCE,
-- поэтому выражение в запросе должно совпадать с индексом буква в букву.
-- id вторым ключом — стабильный порядок при равном времени и задел под
-- чтение батчами (keyset: (expr, id) > ($last_expr, $last_id)).
CREATE INDEX CONCURRENTLY IF NOT EXISTS trips_active_stale_idx
    ON trips ((COALESCE(last_position_at, started_at)), id)
    WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS trips_active_stale_idx;
